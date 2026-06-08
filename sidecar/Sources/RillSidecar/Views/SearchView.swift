// SearchView.swift — entity-anchored recall with a kind filter.

import SwiftUI

struct SearchView: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	@FocusState private var focused: Bool
	@State private var debounce: Task<Void, Never>? = nil

	var body: some View {
		let t = theme.tokens
		VStack(spacing: 10) {
			// Search input
			HStack(spacing: 6) {
				Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundColor(t.textFaint)
				TextField("Recall a memory, person, project…", text: $state.searchQuery)
					.textFieldStyle(.plain).font(.system(size: 13)).foregroundColor(t.text)
					.focused($focused)
					.onChange(of: state.searchQuery) { _ in scheduleSearch() }
					.onSubmit { Task { await state.runSearch() } }
				if !state.searchQuery.isEmpty {
					Button { state.searchQuery = ""; state.searchResults = [] } label: {
						Image(systemName: "xmark.circle.fill").font(.system(size: 12)).foregroundColor(t.textFaint)
					}.buttonStyle(.plain)
				}
			}
			.padding(.horizontal, 10).padding(.vertical, 7)
			.background(RoundedRectangle(cornerRadius: 999).fill(t.surface))
			.overlay(RoundedRectangle(cornerRadius: 999).stroke(t.border, lineWidth: 1))

			// Kind filter
			ScrollView(.horizontal, showsIndicators: false) {
				HStack(spacing: 6) {
					filterChip(label: "all", on: state.searchKind == nil) { state.searchKind = nil; rerun() }
					ForEach(KINDS, id: \.self) { k in
						KindChip(kind: k, selected: state.searchKind == k) {
							state.searchKind = (state.searchKind == k ? nil : k); rerun()
						}
					}
				}
			}

			// Anchored-on entities (from entity-anchored recall) — tap to pivot.
			if !state.searchEntities.isEmpty {
				VStack(alignment: .leading, spacing: 4) {
					Mono("ANCHORED ON", size: 9, weight: .semibold, color: t.textFaint)
					ScrollView(.horizontal, showsIndicators: false) {
						HStack(spacing: 5) {
							ForEach(state.searchEntities) { e in
								Button { state.searchQuery = e.name; Task { await state.runSearch() } } label: {
									HStack(spacing: 3) {
										Circle().fill(t.entity(e.type)).frame(width: 5, height: 5)
										Text("\(ENTITY_SIGIL[e.type] ?? "")\(e.name)")
											.font(.system(size: 10.5, weight: .medium, design: .monospaced))
										if let n = e.mentionCount, n > 0 { Mono("\(n)", size: 9, color: t.textFaint) }
									}
									.padding(.horizontal, 6).padding(.vertical, 2.5)
									.foregroundColor(t.entity(e.type))
									.background(RoundedRectangle(cornerRadius: 999).fill(t.entityBg(e.type)))
									.overlay(RoundedRectangle(cornerRadius: 999).stroke(t.entityLine(e.type), lineWidth: 1))
								}.buttonStyle(.plain)
							}
						}
					}
				}
			}

			// Results
			if state.searching {
				ProgressView().controlSize(.small).frame(maxWidth: .infinity).padding(.vertical, 20)
			} else if state.searchQuery.isEmpty {
				Spacer()
				VStack(spacing: 6) {
					Image(systemName: "magnifyingglass").font(.system(size: 20)).foregroundColor(t.textFaint)
					Text("Search your memory").font(.system(size: 13)).foregroundColor(t.textDim)
				}
				Spacer()
			} else if state.searchResults.isEmpty {
				Spacer()
				Text("Nothing here. Try a different word, or a name.")
					.font(.system(size: 12.5)).foregroundColor(t.textDim).multilineTextAlignment(.center)
				Spacer()
			} else {
				ScrollView {
					HStack { Mono("\(state.searchResults.count) results", size: 10, color: t.textFaint); Spacer() }
						.padding(.bottom, 2)
					LazyVStack(spacing: 1) {
						ForEach(state.searchResults) { MemoryRowView(memory: $0) }
					}
				}
			}
		}
		.padding(12)
		.background(t.bg)
		.onAppear { focused = true }
	}

	private func filterChip(label: String, on: Bool, action: @escaping () -> Void) -> some View {
		let t = theme.tokens
		return Button(action: action) {
			Text(label).font(.system(size: 11.5, weight: .medium, design: .monospaced))
				.padding(.horizontal, 9).padding(.vertical, 4)
				.foregroundColor(on ? t.accent : t.textDim)
				.background(RoundedRectangle(cornerRadius: 999).fill(on ? t.accentBg : t.surface2))
				.overlay(RoundedRectangle(cornerRadius: 999).stroke(on ? t.accent : t.border, lineWidth: 1))
		}.buttonStyle(.plain)
	}

	private func scheduleSearch() {
		debounce?.cancel()
		debounce = Task {
			try? await Task.sleep(nanoseconds: 280_000_000)
			guard !Task.isCancelled else { return }
			await state.runSearch()
		}
	}
	private func rerun() { if !state.searchQuery.isEmpty { Task { await state.runSearch() } } }
}
