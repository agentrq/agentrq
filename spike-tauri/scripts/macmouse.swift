// Posts a click or a drag at global screen points (WP0 S8). Never merge.
//   swift macmouse.swift click X Y | drag X1 Y1 X2 Y2
import CoreGraphics
import Foundation

let a = CommandLine.arguments
func post(_ type: CGEventType, _ x: Double, _ y: Double) {
    let e = CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: CGPoint(x: x, y: y), mouseButton: .left)!
    e.post(tap: .cghidEventTap)
    usleep(30_000)
}
let x = Double(a[2])!, y = Double(a[3])!
post(.mouseMoved, x, y)
post(.leftMouseDown, x, y)
if a[1] == "drag" {
    let x2 = Double(a[4])!, y2 = Double(a[5])!
    for i in 1...10 {
        let t = Double(i) / 10
        post(.leftMouseDragged, x + (x2 - x) * t, y + (y2 - y) * t)
    }
    post(.leftMouseUp, x2, y2)
} else {
    post(.leftMouseUp, x, y)
}
print("ok", CGPreflightPostEventAccess())
