// Renders Kikri's icons from the SVG sources with AppKit (macOS 14+ reads SVG).
// Usage: swift build/icons/render.swift <repo root>
import AppKit

let root = URL(fileURLWithPath: CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ".")
let src = root.appendingPathComponent("build/icons")
let trayOut = root.appendingPathComponent("internal/app/icons")
let iconset = root.appendingPathComponent("build/darwin/Kikri.iconset")

func load(_ name: String) -> NSImage {
    guard let img = NSImage(contentsOf: src.appendingPathComponent(name)) else {
        fatalError("cannot read \(name)")
    }
    return img
}

// Recolours the glyph. Works at 1024pt so the SVG is rasterised sharply
// before being scaled down.
func tinted(_ img: NSImage, _ color: NSColor) -> NSImage {
    let size = NSSize(width: 1024, height: 1024)
    let out = NSImage(size: size)
    out.lockFocus()
    img.draw(in: NSRect(origin: .zero, size: size))
    color.set()
    NSRect(origin: .zero, size: size).fill(using: .sourceAtop)
    out.unlockFocus()
    return out
}

func render(_ px: Int, to url: URL, draw: (NSRect) -> Void) {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px,
                               bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                               colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    rep.size = NSSize(width: px, height: px)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    NSGraphicsContext.current?.imageInterpolation = .high
    draw(NSRect(x: 0, y: 0, width: px, height: px))
    NSGraphicsContext.restoreGraphicsState()
    try! rep.representation(using: .png, properties: [:])!.write(to: url)
}

let mono = load("chick-mono.svg")

// Tray: Wails scales the image to the menu bar thickness, so keep a margin
// like its own 64px defaults (glyph about 17pt in a 22pt bar).
let trayGlyph: (NSImage) -> (NSRect) -> Void = { img in { r in img.draw(in: r.insetBy(dx: r.width * 0.11, dy: r.height * 0.11)) } }
render(64, to: trayOut.appendingPathComponent("tray-template.png"), draw: trayGlyph(tinted(mono, .black)))
render(64, to: trayOut.appendingPathComponent("tray-light.png"), draw: trayGlyph(tinted(mono, NSColor(white: 0.13, alpha: 1))))
render(64, to: trayOut.appendingPathComponent("tray-dark.png"), draw: trayGlyph(tinted(mono, NSColor(white: 0.96, alpha: 1))))

// App icon: dark chick on a light rounded square, following the macOS icon grid
// (824pt body in a 1024pt canvas).
let glyph = tinted(mono, NSColor(white: 0.13, alpha: 1))
func appIcon(_ r: NSRect) {
    let s = r.width / 1024
    let body = NSRect(x: 100 * s, y: 100 * s, width: 824 * s, height: 824 * s)
    let path = NSBezierPath(roundedRect: body, xRadius: 185 * s, yRadius: 185 * s)
    NSGraphicsContext.saveGraphicsState()
    let shadow = NSShadow()
    shadow.shadowColor = NSColor(white: 0, alpha: 0.28)
    shadow.shadowOffset = NSSize(width: 0, height: -10 * s)
    shadow.shadowBlurRadius = 24 * s
    shadow.set()
    NSColor.white.setFill()
    path.fill()
    NSGraphicsContext.restoreGraphicsState()
    NSGradient(starting: .white, ending: NSColor(white: 0.925, alpha: 1))!.draw(in: path, angle: -90)
    glyph.draw(in: body.insetBy(dx: 150 * s, dy: 150 * s))
}
try? FileManager.default.removeItem(at: iconset)
try! FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
for pt in [16, 32, 128, 256, 512] {
    render(pt, to: iconset.appendingPathComponent("icon_\(pt)x\(pt).png"), draw: appIcon)
    render(pt * 2, to: iconset.appendingPathComponent("icon_\(pt)x\(pt)@2x.png"), draw: appIcon)
}
print("ok")
