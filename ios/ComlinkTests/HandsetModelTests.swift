import XCTest
@testable import Comlink

@MainActor final class HandsetModelTests: XCTestCase {
    func testRingRequiresAcceptanceAndClosedClearsText() {
        let model = HandsetModel()
        model.receive(HandsetEvent(type: "ring", call: "one", peer: "peer", text: nil))
        XCTAssertTrue(model.incoming)
        XCTAssertFalse(model.accepted)
        model.receive(HandsetEvent(type: "answer", call: "one", peer: "peer", text: nil))
        model.receive(HandsetEvent(type: "say", call: "other", peer: "peer", text: "wrong call"))
        XCTAssertTrue(model.messages.isEmpty)
        model.receive(HandsetEvent(type: "say", call: "one", peer: "peer", text: "hello"))
        XCTAssertEqual(model.messages.count, 1)
        model.receive(HandsetEvent(type: "hangup", call: "one", peer: "peer", text: nil))
        XCTAssertTrue(model.messages.isEmpty)
        XCTAssertNil(model.call)
    }
    func testFastAuthenticatedReplyDuringAnswerCompletion() {
        let model = HandsetModel()
        model.receive(HandsetEvent(type: "ring", call: "one", peer: "peer", text: nil))
        // The native API has already checked trust, accepted state and ciphertext.
        model.receive(HandsetEvent(type: "say", call: "one", peer: "peer", text: "fast reply"))
        XCTAssertEqual(model.messages.first?.text, "fast reply")
        XCTAssertTrue(model.accepted)
    }
    func testDisconnectErasesConversation() {
        let model = HandsetModel()
        model.receive(HandsetEvent(type: "answer", call: "one", peer: "peer", text: nil))
        model.receive(HandsetEvent(type: "say", call: "one", peer: "peer", text: "hello"))
        model.disconnect()
        XCTAssertTrue(model.messages.isEmpty)
        XCTAssertFalse(model.accepted)
        XCTAssertFalse(model.connected)
    }
}
