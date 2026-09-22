import Foundation
@preconcurrency import ComlinkNative

struct Contact: Codable, Identifiable {
    let name: String
    let number: String
    let communication_approved: Bool
    var id: String { number }
}
struct HandsetEvent: Decodable {
    let type: String
    let call: String?
    let peer: String?
    let text: String?
}
struct Message: Identifiable {
    let id = UUID()
    let text: String
    let mine: Bool
}

/// All Go operations run off the main thread on one serial queue. Polling uses
/// a separate queue, and Go serializes state transitions and contact changes.
@MainActor final class HandsetModel: ObservableObject {
    @Published var number = ""
    @Published var contacts: [Contact] = []
    @Published var messages: [Message] = []
    @Published var status = "Disconnected"
    @Published var error: String?
    @Published var connected = false
    @Published var busy = false
    @Published var call: String?
    @Published var peer = ""
    @Published var incoming = false
    @Published var accepted = false
    private var client: MobileClient?
    private var generation = UUID()
    private var workTask: Task<Void, Never>?
    private var endedCalls: Set<String> = []
    private let operations = DispatchQueue(label: "comlink.operations")
    private let polling = DispatchQueue(label: "comlink.events")

    nonisolated private static func nativeString(_ operation: (NSErrorPointer) -> String) throws -> String {
        var error: NSError?
        let value = operation(&error)
        if let error { throw error }
        return value
    }
    static func storage() throws -> (String, String) {
        let base = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        var directory = base.appendingPathComponent("Comlink", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700, .protectionKey: FileProtectionType.complete])
        var values = URLResourceValues(); values.isExcludedFromBackup = true
        try directory.setResourceValues(values)
        guard let roots = Bundle.main.path(forResource: "comlink-roots", ofType: "json") else { throw CocoaError(.fileNoSuchFile) }
        return (directory.appendingPathComponent("handset.json").path, roots)
    }
    private func run<T>(_ operation: @escaping () throws -> T) async throws -> T {
        try Task.checkCancellation()
        let value: T = try await withCheckedThrowingContinuation { continuation in
            operations.async { continuation.resume(with: Result { try operation() }) }
        }
        try Task.checkCancellation()
        return value
    }
    func connect() {
        guard client == nil, !busy else { return }
        busy = true; error = nil; status = "Connecting…"
        let current = UUID(); generation = current
        workTask = Task {
            do {
                let (profile, roots) = try Self.storage()
                guard let next = MobileNewClient(profile, roots, "https://api.comlink.fyi/mcp") else { throw CocoaError(.coderInvalidValue) }
                client = next
                let identity = try await run { try Self.nativeString { next.open($0) } }
                guard generation == current else { return }
                number = identity; connected = true; status = "Ready · live requests enabled"
                try await reloadContacts(next)
                poll(next, generation: current)
            } catch {
                guard generation == current else { return }
                self.error = error.localizedDescription
                disconnect()
            }
            if generation == current { busy = false }
        }
    }
    func disconnect() {
        generation = UUID()
        workTask?.cancel(); workTask = nil
        let old = client; client = nil
        connected = false; busy = false; status = "Disconnected"; clearCall()
        if let old { operations.async { old.close() } }
    }
    private func clearCall() {
        call = nil; peer = ""; incoming = false; accepted = false; messages.removeAll()
    }
    private func reloadContacts(_ client: MobileClient) async throws {
        let raw = try await run { try Self.nativeString { client.contacts($0) } }
        contacts = try JSONDecoder().decode([Contact].self, from: Data(raw.utf8))
    }
    func save(name: String, number: String, approved: Bool) {
        perform { client in
            try await self.run { try client.saveContact(name, number: number, approved: approved) }
            try await self.reloadContacts(client)
        }
    }
    func dial(_ contact: Contact) {
        perform { client in
            self.messages.removeAll(); self.peer = contact.number; self.incoming = false
            self.status = "Requesting conversation…"
            let id = try await self.run { try Self.nativeString { client.dial(contact.number, error: $0) } }
            guard !self.endedCalls.contains(id) else { return }
            self.call = id
            // An answer may have arrived while the RPC was completing.
            if !self.accepted { self.status = "Waiting for acceptance" }
        }
    }
    func accept(name: String) {
        let number = peer
        let answeringCall = call
        perform { client in
            let saved = self.contacts.first { $0.number == number }
            try await self.run { try client.saveContact(saved?.name ?? name, number: number, approved: true) }
            try await self.reloadContacts(client)
            try await self.run { try client.act("answer", text: "") }
            guard self.call == answeringCall else { return }
            self.accepted = true; self.incoming = false; self.status = "Connected · encrypted"
        }
    }
    func end() {
        let action = incoming ? "reject" : "hangup"
        perform { client in
            try await self.run { try client.act(action, text: "") }
            self.clearCall(); self.status = "Ready"
        }
    }
    func send(_ text: String) {
        let sendingCall = call
        perform { client in
            try await self.run { try client.act("say", text: text) }
            guard self.call == sendingCall else { return }
            self.messages.append(Message(text: text, mine: true))
            self.trimMessages()
        }
    }
    private func trimMessages() {
        if messages.count > 100 { messages.removeFirst(messages.count - 100) }
    }
    private func perform(_ work: @escaping (MobileClient) async throws -> Void) {
        guard let client, connected, !busy else { return }
        busy = true; error = nil
        let current = generation
        workTask = Task {
            do { try await work(client) }
            catch { if generation == current { self.error = error.localizedDescription } }
            if generation == current { busy = false }
            else { clearCall() }
        }
    }
    private func poll(_ client: MobileClient, generation current: UUID) {
        guard generation == current else { return }
        polling.async {
            let result = Result { try Self.nativeString { client.nextEvent($0) } }
            Task { @MainActor in
                guard self.generation == current else { return }
                switch result {
                case .success(let raw):
                    if !raw.isEmpty {
                        do { self.receive(try JSONDecoder().decode(HandsetEvent.self, from: Data(raw.utf8))) }
                        catch { self.error = "Invalid handset event"; self.disconnect() }
                    }
                case .failure(let error): self.error = error.localizedDescription; self.disconnect()
                }
                self.poll(client, generation: current)
            }
        }
    }
    func receive(_ event: HandsetEvent) {
        switch event.type {
        case "ring":
            messages.removeAll(); call = event.call; peer = event.peer ?? ""
            incoming = true; accepted = false; status = "Conversation request"
        case "answer":
            // Events are already authenticated and circuit-bound by Go.
            call = event.call; accepted = true; incoming = false; status = "Connected · encrypted"
        case "say":
            guard call == event.call, let text = event.text else { return }
            // Native only emits speech after local trust and an authenticated
            // accepted circuit. A fast reply can precede the UI's answer completion.
            accepted = true; incoming = false; status = "Connected · encrypted"
            messages.append(Message(text: text, mine: false)); trimMessages()
        case "closed", "reject", "hangup":
            if let id = event.call {
                if endedCalls.count >= 128 { endedCalls.removeAll() }
                endedCalls.insert(id)
            }
            if call == event.call { clearCall(); status = event.type == "reject" ? "Request declined" : "Conversation ended" }
        case "disconnected": disconnect()
        default: break
        }
    }
}
