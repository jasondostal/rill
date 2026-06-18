// main.swift — menu-bar shell. NSStatusItem (real Lucide droplet template image)
// + NSPopover hosting the SwiftUI RootView. ⌥Space global hotkey via Carbon
// (no Accessibility permission required). Accessory app: no Dock icon.

import AppKit
import SwiftUI
import Carbon.HIToolbox

// Lets the SwiftUI layer ask the AppKit shell to close the popover (Esc).
@MainActor
final class PopoverCloser {
	static let shared = PopoverCloser()
	var action: () -> Void = {}
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSPopoverDelegate {
	private var statusItem: NSStatusItem!
	private var popover: NSPopover!
	private var mouseMonitor: Any?
	private var hotKey: HotKey?
	private var pollTimer: Timer?
	private let state = AppState()

	func applicationDidFinishLaunching(_ notification: Notification) {
		// Status bar droplet (template → adapts to light/dark menu bar).
		statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
		if let button = statusItem.button {
			button.image = DropletIcon.menuBarImage(point: 18)
			button.action = #selector(togglePopover)
			button.target = self
			button.toolTip = "Rill — capture a thought (⌥Space)"
		}

		let root = RootView()
			.environmentObject(state)
			.environmentObject(state.theme)
		let hosting = NSHostingController(rootView: root)
		hosting.view.frame = NSRect(x: 0, y: 0, width: 360, height: 532)

		popover = NSPopover()
		popover.contentSize = NSSize(width: 360, height: 532)
		// applicationDefined (not .transient) so opening the detail window doesn't
		// dismiss the capture panel. Dismissal is handled explicitly: the global
		// mouse monitor closes it on a click outside the app, Esc closes it, and
		// the status item toggles it. Clicks on our own detail window are local
		// events, so the monitor won't fire and the panel stays put.
		popover.behavior = .applicationDefined
		popover.animates = true
		popover.contentViewController = hosting
		popover.delegate = self
		DetailWindowManager.shared.configure(state: state, theme: state.theme)
		PopoverCloser.shared.action = { [weak self] in self?.popover.performClose(nil) }

		// Dismiss when the user clicks elsewhere (belt-and-suspenders with .transient).
		mouseMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
			guard let self, self.popover.isShown else { return }
			self.popover.performClose(nil)
		}

		// ⌥Space global summon/dismiss.
		hotKey = HotKey(keyCode: UInt32(kVK_Space), modifiers: UInt32(optionKey)) { [weak self] in
			self?.togglePopover()
		}

		// Preload so the popover opens already populated (and so the data path is
		// exercised at launch, not just on first open).
		SidecarLog.write("launch: configured=\(state.isConfigured) host=\(state.host)")
		Task { await state.refresh() }
	}

	// Poll while the popover is open so memories/theme created elsewhere appear
	// without reopening. Skipped while editing or mid-capture so it can't clobber
	// in-progress input. (A menu-bar popover re-fetches on every open anyway; this
	// just keeps a long-open panel fresh.)
	func popoverDidShow(_ notification: Notification) {
		pollTimer?.invalidate()
		pollTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
			guard let self else { return }
			Task { @MainActor in
				guard self.state.editingID == nil,
				      self.state.draftText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
				await self.state.refresh()
			}
		}
	}

	func popoverDidClose(_ notification: Notification) {
		pollTimer?.invalidate(); pollTimer = nil
	}

	@objc func togglePopover() {
		guard let button = statusItem.button else { return }
		if popover.isShown {
			popover.performClose(nil)
		} else {
			NSApp.activate(ignoringOtherApps: true)
			popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
			popover.contentViewController?.view.window?.makeKey()
			Task { await state.refresh() }
		}
	}
}

/// Carbon RegisterEventHotKey wrapper — works system-wide without Accessibility.
final class HotKey {
	private var ref: EventHotKeyRef?
	private var handlerRef: EventHandlerRef?
	private let onFire: () -> Void

	init?(keyCode: UInt32, modifiers: UInt32, onFire: @escaping () -> Void) {
		self.onFire = onFire
		var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
		let this = Unmanaged.passUnretained(self).toOpaque()
		let installStatus = InstallEventHandler(GetApplicationEventTarget(), { _, _, userData -> OSStatus in
			guard let userData else { return OSStatus(eventNotHandledErr) }
			let hk = Unmanaged<HotKey>.fromOpaque(userData).takeUnretainedValue()
			DispatchQueue.main.async { hk.onFire() }
			return noErr
		}, 1, &spec, this, &handlerRef)
		guard installStatus == noErr else { return nil }
		let id = EventHotKeyID(signature: OSType(0x52494C4C), id: 1) // 'RILL'
		let regStatus = RegisterEventHotKey(keyCode, modifiers, id, GetApplicationEventTarget(), 0, &ref)
		guard regStatus == noErr else { return nil }
	}

	deinit {
		if let ref { UnregisterEventHotKey(ref) }
		if let handlerRef { RemoveEventHandler(handlerRef) }
	}
}

// Entry point — accessory app (menu-bar only).
@main
struct RillSidecarApp {
	@MainActor static func main() {
		let app = NSApplication.shared
		let delegate = AppDelegate()
		app.delegate = delegate
		app.setActivationPolicy(.accessory)
		app.run()
		_ = delegate // keep alive for the lifetime of run()
	}
}
