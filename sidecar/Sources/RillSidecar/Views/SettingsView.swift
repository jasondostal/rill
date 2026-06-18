// SettingsView.swift — connection, appearance (theme), capture defaults.

import SwiftUI

struct SettingsView: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	@State private var showToken = false
	@State private var testing = false
	@State private var testResult: Bool? = nil

	var body: some View {
		let t = theme.tokens
		ScrollView {
			VStack(alignment: .leading, spacing: 18) {
				connection
				appearance
				captureDefaults
				general
				HStack {
					Spacer()
					Mono("rill sidecar · v0.1.0 · a tiny stream", size: 10, color: t.textFaint)
					Spacer()
				}
			}
			.padding(14)
		}
		.background(t.bg)
	}

	// MARK: Connection

	private var connection: some View {
		let t = theme.tokens
		return section("Connection") {
			VStack(alignment: .leading, spacing: 8) {
				field("Host") {
					TextField("https://rill.example.com", text: $state.host)
						.textFieldStyle(.plain).font(.system(size: 12, design: .monospaced))
						.foregroundColor(t.text)
				}
				field("Token") {
					HStack {
						Group {
							if showToken {
								TextField("rill_…", text: $state.token).textFieldStyle(.plain)
							} else {
								SecureField("rill_…", text: $state.token).textFieldStyle(.plain)
							}
						}
						.font(.system(size: 12, design: .monospaced)).foregroundColor(t.text)
						Button { showToken.toggle() } label: {
							Image(systemName: showToken ? "eye.slash" : "eye").font(.system(size: 11))
								.foregroundColor(t.textFaint)
						}.buttonStyle(.plain)
					}
				}
				HStack(spacing: 8) {
					Button {
						testing = true
						Task { let ok = await state.testConnection(); testResult = ok; testing = false }
					} label: {
						Text(testing ? "testing…" : "test").font(.system(size: 12, weight: .medium))
							.padding(.horizontal, 12).padding(.vertical, 5)
							.background(RoundedRectangle(cornerRadius: 6).fill(t.surface2))
							.foregroundColor(t.text)
					}.buttonStyle(.plain).disabled(testing)

					if let r = testResult {
						HStack(spacing: 4) {
							Circle().fill(r ? t.success : t.destructive).frame(width: 6, height: 6)
							Mono(r ? "connected · \(state.memCount) memories" : "failed",
							     size: 10.5, color: r ? t.success : t.destructive)
						}
					} else if state.connected {
						HStack(spacing: 4) {
							Circle().fill(t.success).frame(width: 6, height: 6)
							Mono("connected · \(state.memCount) memories", size: 10.5, color: t.success)
						}
					}
				}
			}
		}
	}

	// MARK: Appearance

	private var appearance: some View {
		let t = theme.tokens
		return section("Appearance") {
			VStack(alignment: .leading, spacing: 10) {
				LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 6), count: 4), spacing: 6) {
					ForEach(ThemeKnobs.presets, id: \.name) { p in
						presetSwatch(p)
					}
				}
				HStack(spacing: 4) {
					Image(systemName: "arrow.triangle.2.circlepath").font(.system(size: 8))
					Mono("synced across the web app + sidecar", size: 9, color: t.textFaint)
				}
				.foregroundColor(t.textFaint)
				slider("accent hue", value: bind(\.accentH), range: 0...360)
				slider("accent chroma", value: bind(\.accentC), range: 0...0.3)
				slider("palette chroma", value: bind(\.catC), range: 0...0.3)
				slider("palette rotate", value: bind(\.hueShift), range: -60...60)
				Toggle(isOn: Binding(
					get: { theme.knobs.mode == .light },
					set: { theme.knobs.mode = $0 ? .light : .dark; state.persistTheme() })) {
					Text("light mode").font(.system(size: 12)).foregroundColor(t.textDim)
				}
				.toggleStyle(.switch).controlSize(.small).tint(t.accent)
			}
		}
	}

	private var general: some View {
		let t = theme.tokens
		return section("Startup") {
			Toggle(isOn: Binding(
				get: { state.launchAtLogin },
				set: { state.setLaunchAtLogin($0) })) {
				VStack(alignment: .leading, spacing: 1) {
					Text("launch at login").font(.system(size: 12)).foregroundColor(t.textDim)
					Mono("opens the droplet when you log in", size: 9.5, color: t.textFaint)
				}
			}
			.toggleStyle(.switch).controlSize(.small).tint(t.accent)
		}
	}

	private func presetSwatch(_ p: ThemeKnobs) -> some View {
		let t = theme.tokens
		let active = theme.activePresetName == p.name
		let accent = oklch(p.accentL, p.accentC, p.accentH)
		let bg = oklch(p.bgL, p.bgC, p.bgH)
		return Button {
			theme.apply(preset: p); state.persistTheme()
		} label: {
			VStack(spacing: 4) {
				ZStack {
					RoundedRectangle(cornerRadius: 5).fill(bg)
					Circle().fill(accent).frame(width: 12, height: 12)
				}
				.frame(height: 30)
				.overlay(RoundedRectangle(cornerRadius: 5).stroke(active ? t.accent : t.border, lineWidth: active ? 1.5 : 1))
				Mono(p.name, size: 8.5, color: active ? t.accent : t.textFaint).lineLimit(1)
			}
		}.buttonStyle(.plain)
	}

	// MARK: Capture defaults

	private var captureDefaults: some View {
		let t = theme.tokens
		return section("Capture defaults") {
			VStack(alignment: .leading, spacing: 8) {
				Mono("default kind", size: 10, color: t.textFaint)
				ScrollView(.horizontal, showsIndicators: false) {
					HStack(spacing: 6) {
						ForEach(KINDS, id: \.self) { k in
							KindChip(kind: k, selected: state.defaultKind == k) {
								state.defaultKind = k; state.draftKind = k
							}
						}
					}
				}
				field("Default project") {
					TextField("rill", text: $state.defaultProject)
						.textFieldStyle(.plain).font(.system(size: 12, design: .monospaced)).foregroundColor(t.text)
				}
			}
		}
	}

	// MARK: helpers

	private func bind(_ kp: WritableKeyPath<ThemeKnobs, Double>) -> Binding<Double> {
		Binding(get: { theme.knobs[keyPath: kp] },
		        set: { theme.knobs[keyPath: kp] = $0; state.persistTheme() })
	}

	private func slider(_ label: String, value: Binding<Double>, range: ClosedRange<Double>) -> some View {
		let t = theme.tokens
		return HStack(spacing: 8) {
			Mono(label, size: 10, color: t.textDim).frame(width: 92, alignment: .leading)
			Slider(value: value, in: range).controlSize(.mini).tint(t.accent)
		}
	}

	private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
		VStack(alignment: .leading, spacing: 8) {
			Mono(title.uppercased(), size: 10, weight: .semibold, color: theme.tokens.textFaint)
			content()
		}
	}

	private func field<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
		let t = theme.tokens
		return VStack(alignment: .leading, spacing: 3) {
			Mono(label, size: 10, color: t.textFaint)
			content()
				.padding(.horizontal, 9).padding(.vertical, 6)
				.background(RoundedRectangle(cornerRadius: 6).fill(t.surface))
				.overlay(RoundedRectangle(cornerRadius: 6).stroke(t.border, lineWidth: 1))
		}
	}
}
