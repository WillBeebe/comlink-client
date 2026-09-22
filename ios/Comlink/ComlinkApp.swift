import SwiftUI

@main
struct ComlinkApp: App {
    @StateObject private var model = HandsetModel()
    @Environment(\.scenePhase) private var phase
    var body: some Scene {
        WindowGroup {
            ContentView(model: model)
                .overlay {
                    if phase != .active { Color(.systemBackground).ignoresSafeArea().overlay(Image(systemName: "lock.fill").font(.largeTitle)) }
                }
                .onChange(of: phase) { _, value in
                    if value != .active { model.disconnect() }
                }
        }
    }
}
