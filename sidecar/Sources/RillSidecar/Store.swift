// Store.swift — observable app state. Owns config, data, capture draft, undo.

import SwiftUI
import Combine
import ServiceManagement

/// Lightweight breadcrumb log to /tmp so runtime state is observable without a UI
/// screenshot. Harmless to keep; useful when iterating on a menu-bar app.
enum SidecarLog {
	static let path = "/tmp/rill-sidecar.log"
	static func write(_ msg: String) {
		NSLog("[rill-sidecar] %@", msg)   // -> unified log (log show ... process RillSidecar)
		let line = "\(Date()) \(msg)\n"
		guard let data = line.data(using: .utf8) else { return }
		if !FileManager.default.fileExists(atPath: path) {
			FileManager.default.createFile(atPath: path, contents: nil)
		}
		if let fh = FileHandle(forWritingAtPath: path) {
			fh.seekToEndOfFile(); fh.write(data); try? fh.close()
		}
	}
}

@MainActor
final class AppState: ObservableObject {
	enum Pane { case home, search, settings }

	// Routing
	@Published var pane: Pane = .home

	// Config
	@Published var host: String { didSet { UserDefaults.standard.set(host, forKey: "rill.host") } }
	@Published var token: String { didSet { UserDefaults.standard.set(token, forKey: "rill.token") } }
	@Published var defaultKind: String { didSet {
		UserDefaults.standard.set(defaultKind, forKey: "rill.defaultKind")
		if defaultKind != oldValue { scheduleCapturePush() }
	} }
	@Published var defaultProject: String { didSet {
		UserDefaults.standard.set(defaultProject, forKey: "rill.defaultProject")
		if defaultProject != oldValue { scheduleCapturePush() }
	} }

	// Launch at login (SMAppService, macOS 13+).
	@Published var launchAtLogin: Bool = (SMAppService.mainApp.status == .enabled)
	func setLaunchAtLogin(_ on: Bool) {
		do {
			if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
			launchAtLogin = (SMAppService.mainApp.status == .enabled)
			SidecarLog.write("launchAtLogin -> \(launchAtLogin)")
		} catch {
			errorMessage = "Launch-at-login: \(error.localizedDescription)"
			launchAtLogin = (SMAppService.mainApp.status == .enabled)
		}
	}

	// Data
	@Published var memories: [Memory] = []
	@Published var entities: [EntityLite] = []
	@Published var loading = false
	@Published var connected = false
	@Published var errorMessage: String?
	@Published var memCount: Int = 0

	// Capture draft
	@Published var draftText: String = ""
	@Published var draftKind: String
	@Published var draftEntities: [EntityLite] = []
	@Published var draftEdges: [EdgeDecl] = []
	@Published var freshID: String? = nil   // row to flash after save

	// Search
	@Published var searchQuery: String = ""
	@Published var searchKind: String? = nil
	@Published var searchResults: [Memory] = []
	@Published var searchEntities: [EntityLite] = []
	@Published var searching = false

	// Row expand / inline edit
	@Published var expandedID: String? = nil
	@Published var editingID: String? = nil
	@Published var detailEntities: [String: [EntityLite]] = [:]

	// Undo (deferred delete: server forget only fires after the window elapses)
	@Published var undo: UndoEntry? = nil
	private var undoTask: Task<Void, Never>? = nil
	struct UndoEntry: Equatable { let memory: Memory; let index: Int }

	let theme: ThemeManager

	var client: RillClient { RillClient(host: host, token: token) }
	var isConfigured: Bool { !host.isEmpty && !token.isEmpty }

	init() {
		let d = UserDefaults.standard
		host = d.string(forKey: "rill.host") ?? "https://rill.example.com"
		// Token lives in the app prefs domain (seeded from .env by build.sh). Same
		// at-rest exposure as the .env it came from; no Keychain ACL prompts, no
		// blocking SecItem call on the launch path.
		token = d.string(forKey: "rill.token") ?? ""
		defaultKind = d.string(forKey: "rill.defaultKind") ?? "fact"
		defaultProject = d.string(forKey: "rill.defaultProject") ?? "rill"
		draftKind = d.string(forKey: "rill.defaultKind") ?? "fact"
		theme = ThemeManager(Self.loadTheme())
	}

	// MARK: - Theme persistence

	private static func loadTheme() -> ThemeKnobs {
		if let data = UserDefaults.standard.data(forKey: "rill.theme"),
		   let k = try? JSONDecoder().decode(ThemeKnobs.self, from: data) { return k }
		return .presets[0]
	}

	private func saveThemeLocal() {
		if let data = try? JSONEncoder().encode(theme.knobs) {
			UserDefaults.standard.set(data, forKey: "rill.theme")
		}
	}

	// Called from the settings UI on every theme tweak: persist locally now,
	// push to the server (debounced) so the theme follows the user cross-device.
	func persistTheme() {
		saveThemeLocal()
		scheduleThemePush()
	}

	private var themePushTask: Task<Void, Never>? = nil
	private func scheduleThemePush() {
		themePushTask?.cancel()
		themePushTask = Task { [weak self] in
			try? await Task.sleep(nanoseconds: 700_000_000)
			guard !Task.isCancelled else { return }
			await self?.pushTheme()
		}
	}

	private func pushTheme() async {
		guard isConfigured,
		      let data = try? JSONEncoder().encode(theme.knobs),
		      let json = String(data: data, encoding: .utf8) else { return }
		do { try await client.updateSetting("appearance.theme", json); SidecarLog.write("theme: pushed to server") }
		catch { SidecarLog.write("theme: push failed (needs admin token?) — \(error.localizedDescription)") }
	}

	private var capturePushTask: Task<Void, Never>? = nil
	private func scheduleCapturePush() {
		capturePushTask?.cancel()
		capturePushTask = Task { [weak self] in
			try? await Task.sleep(nanoseconds: 700_000_000)
			guard !Task.isCancelled else { return }
			guard let self, self.isConfigured else { return }
			try? await self.client.updateSetting("capture.default_kind", self.defaultKind)
			try? await self.client.updateSetting("capture.default_project", self.defaultProject)
			SidecarLog.write("capture defaults pushed")
		}
	}

	// Pull theme + capture defaults from the server (one GET). Applies without
	// re-pushing, so there's no feedback loop.
	func syncSettingsFromServer() async {
		guard isConfigured else { return }
		guard let map = try? await client.getSettingsMap() else { return }
		if let v = map["appearance.theme"], let data = v.data(using: .utf8),
		   let knobs = try? JSONDecoder().decode(ThemeKnobs.self, from: data), knobs != theme.knobs {
			theme.knobs = knobs
			saveThemeLocal()
			SidecarLog.write("theme: synced from server (\(knobs.name))")
		}
		if let k = map["capture.default_kind"], KINDS.contains(k), k != defaultKind {
			defaultKindSilently(k)
		}
		if let p = map["capture.default_project"], p != defaultProject {
			defaultProjectSilently(p)
		}
	}

	// Apply server-sourced capture defaults without triggering a push-back.
	private func defaultKindSilently(_ k: String) {
		capturePushTask?.cancel()
		UserDefaults.standard.set(k, forKey: "rill.defaultKind")
		defaultKind = k; capturePushTask?.cancel()
	}
	private func defaultProjectSilently(_ p: String) {
		capturePushTask?.cancel()
		UserDefaults.standard.set(p, forKey: "rill.defaultProject")
		defaultProject = p; capturePushTask?.cancel()
	}

	// MARK: - Loading

	func refresh() async {
		guard isConfigured else { connected = false; SidecarLog.write("refresh: not configured (host/token empty)"); return }
		loading = true; errorMessage = nil
		do {
			async let mems = client.listMemories(limit: 50)
			async let ents = try? client.listEntities(limit: 200)
			let loaded = try await mems
			memories = loaded
			memCount = loaded.count
			entities = (await ents) ?? []
			connected = true
			SidecarLog.write("refresh: ok — \(loaded.count) memories, \(entities.count) entities (host=\(host))")
			await syncSettingsFromServer()
		} catch {
			errorMessage = error.localizedDescription
			connected = false
			SidecarLog.write("refresh: FAILED — \(error.localizedDescription)")
		}
		loading = false
	}

	func testConnection() async -> Bool {
		let ok = await client.ping()
		connected = ok
		if ok { await refresh() }
		return ok
	}

	// MARK: - Capture

	// Build edge from a manual relate: subject --verb--> object. Predicate is
	// normalized to rill's snake_case style (e.g. "reports to" -> "reports_to").
	func addDraftEdge(subject: EntityLite, verb: String, object: EntityLite) {
		let p = verb.trimmingCharacters(in: .whitespaces).lowercased()
			.replacingOccurrences(of: " ", with: "_")
		guard !p.isEmpty else { return }
		draftEdges.append(EdgeDecl(subject: subject.name, subjectType: subject.type,
		                           predicate: p, object: object.name, objectType: object.type))
	}

	func save() async {
		let text = draftText.trimmingCharacters(in: .whitespacesAndNewlines)
		guard !(text.isEmpty && draftEdges.isEmpty) else { return }
		// rill requires edges to reference *declared* entities, so merge the edge
		// endpoints into the entities[] declarations (deduped by name+type).
		var declMap: [String: EntityDeclPayload] = [:]
		for e in draftEntities { declMap["\(e.type):\(e.name)"] = EntityDeclPayload(name: e.name, type: e.type) }
		for e in draftEdges {
			declMap["\(e.subjectType):\(e.subject)"] = EntityDeclPayload(name: e.subject, type: e.subjectType)
			declMap["\(e.objectType):\(e.object)"] = EntityDeclPayload(name: e.object, type: e.objectType)
		}
		// A relate-only capture (no prose) uses the first triple as the summary.
		let body = text.isEmpty ? draftEdges.map { "\($0.subject) \($0.predicate.replacingOccurrences(of: "_", with: " ")) \($0.object)" }.joined(separator: "; ") : text
		do {
			let res = try await client.remember(
				summary: body.count > 600 ? String(body.prefix(597)) + "…" : body,
				details: body, kind: draftKind,
				project: defaultProject.isEmpty ? nil : defaultProject,
				entities: Array(declMap.values), edges: draftEdges)
			draftText = ""; draftEntities = []; draftEdges = []
			await refresh()
			freshID = res.memory_id
			Task { try? await Task.sleep(nanoseconds: 600_000_000); if freshID == res.memory_id { freshID = nil } }
		} catch {
			errorMessage = error.localizedDescription
		}
	}

	// MARK: - Pin / Forget / Undo

	func togglePin(_ m: Memory) async {
		let want = !m.pinned
		if let i = memories.firstIndex(where: { $0.id == m.id }) { memories[i].pinned = want }
		do { try await client.setPinned(m.id, want) }
		catch {
			errorMessage = error.localizedDescription
			if let i = memories.firstIndex(where: { $0.id == m.id }) { memories[i].pinned = !want }
		}
	}

	func forget(_ m: Memory) {
		guard let index = memories.firstIndex(where: { $0.id == m.id }) else { return }
		memories.remove(at: index)
		undo = UndoEntry(memory: m, index: index)
		undoTask?.cancel()
		let id = m.id
		undoTask = Task { [weak self] in
			try? await Task.sleep(nanoseconds: 5_000_000_000)
			guard !Task.isCancelled else { return }
			await self?.commitForget(id)
		}
	}

	private func commitForget(_ id: String) async {
		do { try await client.forget(id) }
		catch { errorMessage = error.localizedDescription }
		if undo?.memory.id == id { undo = nil }
	}

	func restoreUndo() {
		guard let u = undo else { return }
		undoTask?.cancel(); undoTask = nil
		let i = min(u.index, memories.count)
		memories.insert(u.memory, at: i)
		undo = nil
	}

	// MARK: - Search

	func runSearch() async {
		let q = searchQuery.trimmingCharacters(in: .whitespacesAndNewlines)
		guard !q.isEmpty else { searchResults = []; searchEntities = []; return }
		searching = true
		do {
			let r = try await client.recall(q, kind: searchKind, k: 20)
			searchResults = r.memories
			searchEntities = r.entities
		} catch { errorMessage = error.localizedDescription; searchResults = []; searchEntities = [] }
		searching = false
	}

	// MARK: - Expand / inline edit

	func toggleExpand(_ m: Memory) {
		if expandedID == m.id { expandedID = nil; editingID = nil; return }
		expandedID = m.id; editingID = nil
		if detailEntities[m.id] == nil {
			Task { if let ents = try? await client.memoryDetail(m.id) { detailEntities[m.id] = ents } }
		}
	}

	func loadDetailIfNeeded(_ id: String) {
		guard detailEntities[id] == nil else { return }
		Task { if let ents = try? await client.memoryDetail(id) { detailEntities[id] = ents } }
	}

	// Live lookup by id across home + search lists (detail window stays in sync).
	func memory(byID id: String) -> Memory? {
		memories.first { $0.id == id } ?? searchResults.first { $0.id == id }
	}

	func beginEdit(_ m: Memory) { expandedID = m.id; editingID = m.id }
	func cancelEdit() { editingID = nil }

	func saveEdit(_ id: String, summary: String, details: String) async {
		let s = summary.trimmingCharacters(in: .whitespacesAndNewlines)
		guard !s.isEmpty else { return }
		do {
			try await client.editMemory(id, summary: s, details: details)
			func patch(_ arr: inout [Memory]) {
				if let i = arr.firstIndex(where: { $0.id == id }) { arr[i].summary = s; arr[i].details = details }
			}
			patch(&memories); patch(&searchResults)
			editingID = nil
		} catch { errorMessage = error.localizedDescription }
	}

	// MARK: - Entity autocomplete

	func entityMatches(_ query: String, limit: Int = 6) -> [EntityLite] {
		let q = query.lowercased()
		guard !q.isEmpty else { return Array(entities.prefix(limit)) }
		return entities.filter { $0.name.lowercased().contains(q) }.prefix(limit).map { $0 }
	}
}

// Relative time: "3m", "2h", "5d", or ISO date.
func relativeTime(_ iso: String?) -> String {
	guard let iso, let date = isoDate(iso) else { return "" }
	let s = Int(Date().timeIntervalSince(date))
	if s < 60 { return "\(max(0, s))s" }
	if s < 3600 { return "\(s / 60)m" }
	if s < 86400 { return "\(s / 3600)h" }
	if s < 86400 * 7 { return "\(s / 86400)d" }
	return String(iso.prefix(10))
}

private let isoFormatters: [ISO8601DateFormatter] = {
	let a = ISO8601DateFormatter(); a.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
	let b = ISO8601DateFormatter(); b.formatOptions = [.withInternetDateTime]
	return [a, b]
}()

private func isoDate(_ s: String) -> Date? {
	for f in isoFormatters { if let d = f.date(from: s) { return d } }
	return nil
}
