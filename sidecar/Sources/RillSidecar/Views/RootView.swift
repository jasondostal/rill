// RootView.swift — the popover: header · body (home/search/settings) · footer,
// with an undo toast overlay.

import SwiftUI

struct RootView: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState

	var body: some View {
		let t = theme.tokens
		VStack(spacing: 0) {
			header
			Divider().overlay(t.border)
			pane
			Divider().overlay(t.border)
			footer
		}
		.frame(width: 360, height: 532)
		.background(t.bg)
		.overlay(alignment: .bottom) {
			if let u = state.undo { UndoToast(entry: u) }
		}
		.task { if state.memories.isEmpty { await state.refresh() } }
		.environment(\.colorScheme, t.isLight ? .light : .dark)
	}

	private var header: some View {
		let t = theme.tokens
		return HStack(spacing: 8) {
			if state.pane == .settings || state.pane == .search {
				IconButton(system: "chevron.left") { state.pane = .home }
			}
			Logo()
			if state.pane == .home {
				HStack(spacing: 4) {
					Circle().fill(state.connected ? t.success : t.warning).frame(width: 6, height: 6)
					Mono(state.connected ? "synced" : "offline", size: 10,
					     color: state.connected ? t.success : t.warning)
				}
			}
			Spacer()
			IconButton(system: "magnifyingglass", active: state.pane == .search) {
				state.pane = state.pane == .search ? .home : .search
			}
			IconButton(system: "gearshape", active: state.pane == .settings) {
				state.pane = state.pane == .settings ? .home : .settings
			}
		}
		.padding(.horizontal, 12).padding(.vertical, 9)
		.frame(height: 42)
		.background(t.surface.opacity(0.4))
	}

	@ViewBuilder private var pane: some View {
		switch state.pane {
		case .home: CaptureView()
		case .search: SearchView()
		case .settings: SettingsView()
		}
	}

	private var footer: some View {
		let t = theme.tokens
		return HStack(spacing: 12) {
			hint("⌘K", "search")
			hint("⌘↵", "save")
			hint("⌥Space", "summon")
			Spacer()
			// Hidden shortcut sinks.
			Button("") { state.pane = .search }.keyboardShortcut("k", modifiers: .command).hidden().frame(width: 0)
			Button("") { if state.pane == .home { PopoverCloser.shared.action() } else { state.pane = .home } }
				.keyboardShortcut(.escape, modifiers: []).hidden().frame(width: 0)
			Button("") { Task { await state.refresh() } }.keyboardShortcut("r", modifiers: .command).hidden().frame(width: 0)
		}
		.padding(.horizontal, 12).padding(.vertical, 6)
		.frame(height: 26)
		.background(t.surface.opacity(0.4))
	}

	private func hint(_ key: String, _ label: String) -> some View {
		HStack(spacing: 3) {
			Mono(key, size: 9, weight: .semibold, color: theme.tokens.textDim)
			Mono(label, size: 9, color: theme.tokens.textFaint)
		}
	}
}

struct UndoToast: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	let entry: AppState.UndoEntry
	var body: some View {
		let t = theme.tokens
		HStack(spacing: 10) {
			Image(systemName: "trash").font(.system(size: 11)).foregroundColor(t.textDim)
			Text("Forgot “\(String(entry.memory.summary.prefix(32)))\(entry.memory.summary.count > 32 ? "…" : "")”")
				.font(.system(size: 12)).foregroundColor(t.text).lineLimit(1)
			Spacer()
			Button { state.restoreUndo() } label: {
				Text("Undo").font(.system(size: 12, weight: .semibold)).foregroundColor(t.accent)
			}.buttonStyle(.plain)
		}
		.padding(.horizontal, 12).padding(.vertical, 9)
		.background(RoundedRectangle(cornerRadius: 8).fill(t.surface3))
		.overlay(RoundedRectangle(cornerRadius: 8).stroke(t.borderStrong, lineWidth: 1))
		.shadow(color: .black.opacity(0.3), radius: 12, y: 4)
		.padding(.horizontal, 12).padding(.bottom, 34)
		.transition(.move(edge: .bottom).combined(with: .opacity))
		.animation(.spring(response: 0.3, dampingFraction: 0.8), value: state.undo)
	}
}
