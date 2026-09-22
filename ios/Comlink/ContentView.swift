import SwiftUI

struct ContentView: View {
    @ObservedObject var model: HandsetModel
    @State private var adding = false
    @State private var name = ""
    @State private var number = ""
    @State private var draft = ""
    @State private var accepting = false
    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                HStack {
                    Circle().fill(model.connected ? .green : .secondary).frame(width: 7, height: 7)
                    Text(model.status).font(.caption).foregroundStyle(.secondary)
                    Spacer()
                    if model.busy { ProgressView() }
                }.padding()
                if model.call != nil { conversation } else { contacts }
            }
            .navigationTitle("Comlink")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button(model.connected ? "Disconnect" : "Connect") {
                        if model.connected { model.disconnect() } else { model.connect() }
                    }.disabled(model.busy)
                }
            }
            .sheet(isPresented: $adding) {
                NavigationStack {
                    Form {
                        TextField("Name", text: $name).textContentType(.nickname)
                        TextField("Comlink number", text: $number, axis: .vertical)
                            .textInputAutocapitalization(.never).autocorrectionDisabled()
                        Text("Use a 128-character Comlink number, not an SMS number. Verify it with the person you intend to contact. Trust permits communication only.")
                            .font(.footnote).foregroundStyle(.secondary)
                        Button("Trust and add contact") {
                            model.save(name: name, number: number, approved: true); adding = false
                        }.disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || number.isEmpty)
                    }
                    .navigationTitle("Add contact")
                    .toolbar { Button("Cancel") { adding = false } }
                }
            }
            .alert("Accept and trust this number?", isPresented: $accepting) {
                TextField("Contact name", text: $name)
                Button("Accept and trust") { model.accept(name: name) }
                Button("Cancel", role: .cancel) {}
            } message: { Text("Check the full number with your contact first. Accepting opens an encrypted live conversation.") }
            .alert("Comlink", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
                Button("OK") { model.error = nil }
            } message: { Text(model.error ?? "") }
            .onChange(of: model.call) { _, _ in draft = "" }
        }
    }
    private var contacts: some View {
        List {
            Section("Your number") {
                if model.number.isEmpty {
                    Text("Connect to create your number.").foregroundStyle(.secondary)
                } else {
                    Text(model.number).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                    ShareLink("Share number", item: model.number)
                }
                Text("Both people must keep Comlink open and connected. Incoming requests require your acceptance. There is no offline delivery or message history.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Section("Contacts") {
                ForEach(model.contacts) { contact in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text(contact.name).font(.headline)
                            Spacer()
                            if contact.communication_approved {
                                Button("Message") { model.dial(contact) }.buttonStyle(.bordered)
                                    .disabled(!model.connected || model.busy)
                            } else { Text("Blocked").font(.caption).foregroundStyle(.secondary) }
                        }
                        Text(contact.number).font(.system(.caption2, design: .monospaced)).textSelection(.enabled)
                    }
                    .swipeActions {
                        Button(contact.communication_approved ? "Block" : "Trust") {
                            model.save(name: contact.name, number: contact.number, approved: !contact.communication_approved)
                        }.tint(contact.communication_approved ? .red : .green).disabled(!model.connected || model.busy)
                    }
                }
                Button { name = ""; number = ""; adding = true } label: { Label("Add a number", systemImage: "plus") }
                    .disabled(!model.connected || model.busy)
            }
        }
    }
    private var conversation: some View {
        VStack(spacing: 12) {
            Text(model.contacts.first { $0.number == model.peer }?.name ?? "New contact").font(.headline)
            Text(model.peer).font(.system(.caption2, design: .monospaced)).textSelection(.enabled).padding(.horizontal)
            if model.incoming {
                Text("This number wants to talk.").foregroundStyle(.secondary)
                HStack {
                    Button("Decline", role: .destructive) { model.end() }
                    Button("Accept and trust") {
                        name = model.contacts.first { $0.number == model.peer }?.name ?? ""
                        accepting = true
                    }.buttonStyle(.borderedProminent)
                }.disabled(model.busy)
            }
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 12) {
                        ForEach(model.messages) { message in
                            HStack {
                                if message.mine { Spacer(minLength: 40) }
                                Text(message.text).padding(12)
                                    .background(message.mine ? Color.accentColor.opacity(0.15) : Color.secondary.opacity(0.1))
                                    .clipShape(RoundedRectangle(cornerRadius: 16))
                                if !message.mine { Spacer(minLength: 40) }
                            }.id(message.id)
                        }
                    }.padding()
                }.onChange(of: model.messages.count) { _, _ in
                    if let last = model.messages.last { proxy.scrollTo(last.id, anchor: .bottom) }
                }
            }
            if model.accepted {
                HStack(alignment: .bottom) {
                    TextField("Message", text: $draft, axis: .vertical).lineLimit(1...5).textFieldStyle(.roundedBorder)
                    Button {
                        let text = draft; draft = ""; model.send(text)
                    } label: { Image(systemName: "arrow.up.circle.fill").font(.title) }
                        .accessibilityLabel("Send message")
                        .disabled(model.busy || draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || draft.utf8.count > 4096)
                }.padding(.horizontal)
            }
            if !model.incoming { Button("End conversation", role: .destructive) { model.end() }.disabled(model.busy) }
            Text("Live only · cleared when you leave").font(.caption2).foregroundStyle(.secondary).padding(.bottom)
        }
    }
}
