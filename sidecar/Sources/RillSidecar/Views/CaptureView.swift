// CaptureView.swift — the home/hero: capture card + recent list (or empty state).

import SwiftUI

struct CaptureView: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	@FocusState private var captureFocused: Bool

	var body: some View {
		let t = theme.tokens
		VStack(spacing: 0) {
			// Pinned capture surface — the primary action stays put; only the
			// recent list below scrolls (matches the design + quick-capture norm).
			VStack(alignment: .leading, spacing: 12) {
				captureCard
				recentHeader
			}
			.padding(.horizontal, 12).padding(.top, 12).padding(.bottom, 8)

			ScrollView {
				VStack(alignment: .leading, spacing: 0) {
					if state.loading && state.memories.isEmpty {
						ProgressView().controlSize(.small).frame(maxWidth: .infinity).padding(.vertical, 24)
					} else if let err = state.errorMessage, state.memories.isEmpty {
						errorBlock(err)
					} else if state.memories.isEmpty {
						EmptyState()
					} else {
						LazyVStack(spacing: 1) {
							ForEach(state.memories) { MemoryRowView(memory: $0) }
						}
					}
				}
				.padding(.horizontal, 12).padding(.bottom, 12)
			}
		}
		.background(t.bg)
		.onAppear { captureFocused = true }
	}

	// MARK: Capture card

	private var captureCard: some View {
		let t = theme.tokens
		return VStack(alignment: .leading, spacing: 9) {
			ZStack(alignment: .topLeading) {
				if state.draftText.isEmpty {
					Text("Remember something…  (⌘↵ to save)")
						.font(.system(size: 13.5))
						.foregroundColor(t.textFaint)
						.padding(.horizontal, 5).padding(.top, 8)
						.allowsHitTesting(false)
				}
				TextEditor(text: $state.draftText)
					.font(.system(size: 13.5))
					.foregroundColor(t.text)
					.scrollContentBackground(.hidden)
					.frame(minHeight: 58, maxHeight: 120)
					.focused($captureFocused)
			}

			if !state.draftEntities.isEmpty {
				FlowChips(state.draftEntities) { ent in
					EntityChip(entity: ent) {
						state.draftEntities.removeAll { $0.id == ent.id }
					}
				}
			}

			if !state.draftEdges.isEmpty {
				VStack(alignment: .leading, spacing: 4) {
					ForEach(Array(state.draftEdges.enumerated()), id: \.offset) { idx, edge in
						TripleRow(edge: edge) { state.draftEdges.remove(at: idx) }
					}
				}
			}

			// Kind selector
			ScrollView(.horizontal, showsIndicators: false) {
				HStack(spacing: 6) {
					ForEach(KINDS, id: \.self) { k in
						KindChip(kind: k, selected: state.draftKind == k) { state.draftKind = k }
					}
				}
				.padding(.bottom, 2)
			}

			Divider().overlay(t.border)

			HStack(spacing: 6) {
				EntityLinkButton().fixedSize()
				RelateBuilder().fixedSize()
				Spacer(minLength: 4)
				Mono("\(state.draftKind) · \(state.defaultProject.isEmpty ? "—" : state.defaultProject)",
				     size: 10.5, color: t.textFaint)
					.lineLimit(1).layoutPriority(-1)
				saveButton.fixedSize()
			}
		}
		.padding(11)
		.background(RoundedRectangle(cornerRadius: 7).fill(t.bg))
		.overlay(RoundedRectangle(cornerRadius: 7).stroke(t.borderStrong, lineWidth: 1))
	}

	private var saveButton: some View {
		let t = theme.tokens
		let active = !state.draftText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !state.draftEdges.isEmpty
		return Button { Task { await state.save() } } label: {
			HStack(spacing: 4) {
				Mono("⌘↵", size: 10, weight: .semibold, color: active ? t.accentFg : t.textFaint)
				Text("Save").font(.system(size: 12, weight: .semibold))
					.foregroundColor(active ? t.accentFg : t.textFaint)
			}
			.padding(.horizontal, 10).padding(.vertical, 5)
			.background(RoundedRectangle(cornerRadius: 6).fill(active ? t.accent : t.surface2))
		}
		.buttonStyle(.plain)
		.disabled(!active)
		.keyboardShortcut(.return, modifiers: .command)
	}

	private var recentHeader: some View {
		HStack(spacing: 6) {
			Mono("RECENT", size: 10, weight: .semibold, color: theme.tokens.textFaint)
			if !state.memories.isEmpty {
				Mono("\(state.memories.count)", size: 10, color: theme.tokens.textFaint)
			}
			Spacer()
		}
		.padding(.top, 2)
	}

	private func errorBlock(_ msg: String) -> some View {
		let t = theme.tokens
		return VStack(spacing: 6) {
			Image(systemName: "wifi.exclamationmark").font(.system(size: 22)).foregroundColor(t.destructive)
			Text("Can't reach rill").font(.system(size: 14, weight: .semibold)).foregroundColor(t.text)
			Text(msg).font(.system(size: 11)).foregroundColor(t.textDim)
				.multilineTextAlignment(.center).lineLimit(3)
			Button("Open settings") { state.pane = .settings }
				.font(.system(size: 12)).foregroundColor(t.accent).buttonStyle(.plain)
		}
		.frame(maxWidth: .infinity).padding(.vertical, 28)
	}
}

// MARK: - Entity link popover

struct EntityLinkButton: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	@State private var open = false
	@State private var query = ""

	var body: some View {
		let t = theme.tokens
		Button { open.toggle() } label: {
			HStack(spacing: 4) {
				Image(systemName: "at").font(.system(size: 10, weight: .semibold))
				Text("link").font(.system(size: 11, weight: .medium, design: .monospaced))
			}
			.foregroundColor(t.textDim)
			.padding(.horizontal, 8).padding(.vertical, 4)
			.overlay(RoundedRectangle(cornerRadius: 999)
				.stroke(style: StrokeStyle(lineWidth: 1, dash: [3, 2])).foregroundColor(t.border))
		}
		.buttonStyle(.plain)
		.popover(isPresented: $open, arrowEdge: .bottom) {
			VStack(alignment: .leading, spacing: 6) {
				TextField("link a person, project…", text: $query)
					.textFieldStyle(.roundedBorder).font(.system(size: 12)).frame(width: 220)
				ScrollView {
					VStack(alignment: .leading, spacing: 2) {
						ForEach(state.entityMatches(query, limit: 8)) { ent in
							Button {
								if !state.draftEntities.contains(where: { $0.id == ent.id }) {
									state.draftEntities.append(ent)
								}
								query = ""; open = false
							} label: {
								HStack(spacing: 5) {
									Circle().fill(t.entity(ent.type)).frame(width: 6, height: 6)
									Text("\(ENTITY_SIGIL[ent.type] ?? "")\(ent.name)").font(.system(size: 12))
										.foregroundColor(t.text)
									Spacer()
									Mono(ent.type, size: 9, color: t.textFaint)
								}
								.padding(.horizontal, 6).padding(.vertical, 4)
								.contentShape(Rectangle())
							}
							.buttonStyle(.plain)
						}
						if !query.isEmpty {
							Divider().overlay(t.border).padding(.vertical, 2)
							CreateEntityRow(name: query) { ent in
								if !state.draftEntities.contains(where: { $0.name == ent.name && $0.type == ent.type }) {
									state.draftEntities.append(ent)
								}
								query = ""; open = false
							}
						} else if state.entityMatches(query, limit: 8).isEmpty {
							Mono("type to search or create", size: 10, color: t.textFaint).padding(6)
						}
					}
				}
				.frame(width: 220, height: 180)
			}
			.padding(8)
			.background(t.surface)
		}
	}
}

// MARK: - Relate builder (manual subject --verb--> object edge)

struct RelateBuilder: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	@State private var open = false
	@State private var subject: EntityLite? = nil
	@State private var object: EntityLite? = nil
	@State private var verb = ""

	private let quickVerbs = ["uses", "prefers", "reports_to", "depends_on", "works_on", "created", "maintains", "part_of"]

	var body: some View {
		let t = theme.tokens
		Button { open.toggle() } label: {
			HStack(spacing: 4) {
				Image(systemName: "arrow.left.arrow.right").font(.system(size: 9, weight: .semibold))
				Text("relate").font(.system(size: 11, weight: .medium, design: .monospaced))
			}
			.foregroundColor(t.textDim)
			.padding(.horizontal, 8).padding(.vertical, 4)
			.overlay(RoundedRectangle(cornerRadius: 999)
				.stroke(style: StrokeStyle(lineWidth: 1, dash: [3, 2])).foregroundColor(t.border))
		}
		.buttonStyle(.plain)
		.popover(isPresented: $open, arrowEdge: .bottom) {
			VStack(alignment: .leading, spacing: 8) {
				Mono("relate two things", size: 10, weight: .semibold, color: t.textFaint)
				EntityPickerInline(placeholder: "subject…", selection: $subject)
				VStack(alignment: .leading, spacing: 4) {
					TextField("verb (e.g. uses, reports to)", text: $verb)
						.textFieldStyle(.roundedBorder).font(.system(size: 12))
					ScrollView(.horizontal, showsIndicators: false) {
						HStack(spacing: 4) {
							ForEach(quickVerbs, id: \.self) { v in
								Button { verb = v } label: {
									Text(v.replacingOccurrences(of: "_", with: " "))
										.font(.system(size: 9.5, design: .monospaced))
										.padding(.horizontal, 6).padding(.vertical, 2)
										.foregroundColor(verb == v ? t.accent : t.textDim)
										.background(RoundedRectangle(cornerRadius: 999).fill(verb == v ? t.accentBg : t.surface2))
								}.buttonStyle(.plain)
							}
						}
					}
				}
				EntityPickerInline(placeholder: "object…", selection: $object)
				Button {
					if let s = subject, let o = object, !verb.trimmingCharacters(in: .whitespaces).isEmpty {
						state.addDraftEdge(subject: s, verb: verb, object: o)
						subject = nil; object = nil; verb = ""; open = false
					}
				} label: {
					Text("add edge").font(.system(size: 12, weight: .semibold))
						.frame(maxWidth: .infinity).padding(.vertical, 5)
						.foregroundColor(canAdd ? t.accentFg : t.textFaint)
						.background(RoundedRectangle(cornerRadius: 6).fill(canAdd ? t.accent : t.surface2))
				}
				.buttonStyle(.plain).disabled(!canAdd)
			}
			.padding(10).frame(width: 250)
			.background(t.surface)
		}
	}

	private var canAdd: Bool { subject != nil && object != nil && !verb.trimmingCharacters(in: .whitespaces).isEmpty }
}

// Pick one entity: shows the selection as a chip, or a filter field + matches.
struct EntityPickerInline: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	let placeholder: String
	@Binding var selection: EntityLite?
	@State private var query = ""

	var body: some View {
		let t = theme.tokens
		if let sel = selection {
			HStack(spacing: 5) {
				EntityChip(entity: sel) { selection = nil }
				Spacer()
			}
		} else {
			VStack(alignment: .leading, spacing: 3) {
				TextField(placeholder, text: $query).textFieldStyle(.roundedBorder).font(.system(size: 12))
				if !query.isEmpty {
					ScrollView {
						VStack(alignment: .leading, spacing: 1) {
							ForEach(state.entityMatches(query, limit: 6)) { e in
								Button { selection = e; query = "" } label: {
									HStack(spacing: 5) {
										Circle().fill(t.entity(e.type)).frame(width: 6, height: 6)
										Text("\(ENTITY_SIGIL[e.type] ?? "")\(e.name)").font(.system(size: 12)).foregroundColor(t.text)
										Spacer()
										Mono(e.type, size: 9, color: t.textFaint)
									}
									.padding(.horizontal, 5).padding(.vertical, 3).contentShape(Rectangle())
								}.buttonStyle(.plain)
							}
						}
					}
					.frame(height: 78)
					CreateEntityRow(name: query) { ent in selection = ent; query = "" }
				}
			}
		}
	}
}

// "Create '<name>' as [type]" — lets a relate/link reference an entity that
// doesn't exist yet. rill's remember() resolver creates any declared entity
// that doesn't match, so the client only needs name + type.
struct CreateEntityRow: View {
	@EnvironmentObject var theme: ThemeManager
	let name: String
	let onCreate: (EntityLite) -> Void
	var body: some View {
		let t = theme.tokens
		VStack(alignment: .leading, spacing: 3) {
			Mono("create “\(name)” as", size: 9.5, color: t.textFaint)
			ScrollView(.horizontal, showsIndicators: false) {
				HStack(spacing: 4) {
					ForEach(ENTITY_TYPES, id: \.self) { ty in
						Button { onCreate(EntityLite(id: "new:\(ty):\(name)", name: name, type: ty)) } label: {
							HStack(spacing: 3) {
								Circle().fill(t.entity(ty)).frame(width: 5, height: 5)
								Text("\(ENTITY_SIGIL[ty] ?? "")\(ty)").font(.system(size: 9.5, design: .monospaced))
							}
							.padding(.horizontal, 6).padding(.vertical, 3)
							.foregroundColor(t.entity(ty))
							.background(RoundedRectangle(cornerRadius: 999).fill(t.entityBg(ty)))
							.overlay(RoundedRectangle(cornerRadius: 999).stroke(t.entityLine(ty), lineWidth: 1))
						}.buttonStyle(.plain)
					}
				}
			}
		}
	}
}

// Simple wrapping chip row.
struct FlowChips<Item: Identifiable, Content: View>: View {
	let items: [Item]
	let content: (Item) -> Content
	init(_ items: [Item], @ViewBuilder content: @escaping (Item) -> Content) {
		self.items = items; self.content = content
	}
	var body: some View {
		ScrollView(.horizontal, showsIndicators: false) {
			HStack(spacing: 5) { ForEach(items) { content($0) } }
		}
	}
}
