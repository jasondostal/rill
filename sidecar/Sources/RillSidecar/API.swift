// API.swift — direct REST client for a running Rill server. Native URLSession,
// so no CORS and no proxy: the popover talks straight to rill.example.com with a
// Bearer PAT. Field names mirror internal/memory/*.go json tags exactly.

import Foundation

// MARK: - Wire models

/// One memory, tolerant enough to decode both /api/memories rows (MemoryRow)
/// and /api/recall hits (MemoryHit), which omit pinned/is_active.
struct Memory: Identifiable, Codable, Equatable {
	let id: String
	var summary: String
	var details: String?
	var kind: String
	var tags: [String]?
	var author: String
	var project: String?
	var valence: String?
	var pinned: Bool
	var isActive: Bool
	var createdAt: String?

	enum CodingKeys: String, CodingKey {
		case id, summary, details, kind, tags, author, project, valence, pinned
		case isActive = "is_active"
		case createdAt = "created_at"
	}

	init(from decoder: Decoder) throws {
		let c = try decoder.container(keyedBy: CodingKeys.self)
		id = try c.decode(String.self, forKey: .id)
		summary = try c.decode(String.self, forKey: .summary)
		details = try c.decodeIfPresent(String.self, forKey: .details)
		kind = try c.decode(String.self, forKey: .kind)
		tags = try c.decodeIfPresent([String].self, forKey: .tags)
		author = (try? c.decode(String.self, forKey: .author)) ?? ""
		project = try c.decodeIfPresent(String.self, forKey: .project)
		valence = try c.decodeIfPresent(String.self, forKey: .valence)
		pinned = (try? c.decodeIfPresent(Bool.self, forKey: .pinned)) ?? false
		isActive = (try? c.decodeIfPresent(Bool.self, forKey: .isActive)) ?? true
		createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt)
	}

	// Local construction (drafts / optimistic) — not used for decoding.
	init(id: String, summary: String, kind: String, author: String,
	     project: String? = nil, pinned: Bool = false, createdAt: String? = nil,
	     details: String? = nil, tags: [String]? = nil) {
		self.id = id; self.summary = summary; self.kind = kind; self.author = author
		self.project = project; self.pinned = pinned; self.createdAt = createdAt
		self.details = details; self.tags = tags; self.isActive = true; self.valence = nil
	}

	func encode(to encoder: Encoder) throws {
		var c = encoder.container(keyedBy: CodingKeys.self)
		try c.encode(id, forKey: .id); try c.encode(summary, forKey: .summary)
		try c.encode(kind, forKey: .kind); try c.encode(author, forKey: .author)
		try c.encodeIfPresent(project, forKey: .project); try c.encode(pinned, forKey: .pinned)
		try c.encodeIfPresent(createdAt, forKey: .createdAt)
	}
}

struct EntityLite: Identifiable, Codable, Equatable, Hashable {
	let id: String
	let name: String
	let type: String
	var mentionCount: Int?
	enum CodingKeys: String, CodingKey { case id, name, type, mentionCount = "mention_count" }
}

struct EdgeDecl: Codable {
	var subject: String
	var subjectType: String
	var predicate: String
	var object: String
	var objectType: String
	var valence: String?
	enum CodingKeys: String, CodingKey {
		case subject, predicate, object, valence
		case subjectType = "subject_type"
		case objectType = "object_type"
	}
}

struct EntityDeclPayload: Codable {
	var name: String
	var type: String
}

private struct MemoriesResponse: Codable { let memories: [Memory] }
private struct RecallResponse: Codable { let memories: [Memory]; let entities: [EntityLite]? }
private struct EntitiesResponse: Codable { let entities: [EntityLite] }
struct RememberResult: Codable { let memory_id: String }

// MARK: - Client

enum RillError: LocalizedError {
	case http(Int, String)
	case noConfig
	case transport(String)
	var errorDescription: String? {
		switch self {
		case .http(let code, let msg): return "rill \(code): \(msg)"
		case .noConfig: return "no host/token configured"
		case .transport(let m): return m
		}
	}
}

struct RillClient {
	let host: String   // e.g. https://rill.example.com
	let token: String

	private var base: URL? { URL(string: host.replacingOccurrences(of: "/+$", with: "", options: .regularExpression)) }

	private func request(_ method: String, _ path: String, body: Data? = nil) async throws -> Data {
		guard !host.isEmpty, !token.isEmpty, let root = base,
		      let url = URL(string: path, relativeTo: root) else { throw RillError.noConfig }
		var req = URLRequest(url: url)
		req.httpMethod = method
		req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
		req.setValue("application/json", forHTTPHeaderField: "Content-Type")
		req.httpBody = body
		req.timeoutInterval = 12
		do {
			let (data, resp) = try await URLSession.shared.data(for: req)
			let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
			guard (200..<300).contains(code) else {
				let msg = String(data: data, encoding: .utf8) ?? ""
				throw RillError.http(code, String(msg.prefix(200)))
			}
			return data
		} catch let e as RillError {
			throw e
		} catch {
			throw RillError.transport(error.localizedDescription)
		}
	}

	// --- Connection ---
	func ping() async -> Bool {
		(try? await request("GET", "/api/ping")) != nil
	}

	// --- Memories ---
	func listMemories(limit: Int = 50) async throws -> [Memory] {
		let data = try await request("GET", "/api/memories?limit=\(limit)")
		return try JSONDecoder().decode(MemoriesResponse.self, from: data).memories
	}

	func recall(_ query: String, kind: String? = nil, k: Int = 20) async throws -> (memories: [Memory], entities: [EntityLite]) {
		var body: [String: Any] = ["query": query, "k": k]
		if let kind { body["kind"] = kind }
		let data = try await request("POST", "/api/recall",
			body: try JSONSerialization.data(withJSONObject: body))
		let r = try JSONDecoder().decode(RecallResponse.self, from: data)
		return (r.memories, r.entities ?? [])
	}

	/// Entities mentioned by one memory (for the expanded row). Other fields we
	/// already have from the list row.
	func memoryDetail(_ id: String) async throws -> [EntityLite] {
		let data = try await request("GET", "/api/memory/\(idTail(id))")
		struct R: Codable { let mentioned_entities: [EntityLite]? }
		return (try JSONDecoder().decode(R.self, from: data)).mentioned_entities ?? []
	}

	func editMemory(_ id: String, summary: String?, details: String?) async throws {
		var patch: [String: Any] = [:]
		if let summary { patch["summary"] = summary }
		if let details { patch["details"] = details }
		guard !patch.isEmpty else { return }
		_ = try await request("PATCH", "/api/memory/\(idTail(id))",
			body: try JSONSerialization.data(withJSONObject: patch))
	}

	@discardableResult
	func remember(summary: String, details: String?, kind: String, project: String?,
	              entities: [EntityDeclPayload], edges: [EdgeDecl]) async throws -> RememberResult {
		var body: [String: Any] = ["summary": summary, "kind": kind]
		body["details"] = (details?.isEmpty == false ? details! : summary)
		if let project, !project.isEmpty { body["project"] = project }
		if !entities.isEmpty { body["entities"] = entities.map { ["name": $0.name, "type": $0.type] } }
		if !edges.isEmpty {
			body["edges"] = edges.map { e -> [String: Any] in
				var d: [String: Any] = ["subject": e.subject, "subject_type": e.subjectType,
				                        "predicate": e.predicate, "object": e.object, "object_type": e.objectType]
				if let v = e.valence { d["valence"] = v }
				return d
			}
		}
		let data = try await request("POST", "/api/remember",
			body: try JSONSerialization.data(withJSONObject: body))
		return try JSONDecoder().decode(RememberResult.self, from: data)
	}

	func setPinned(_ id: String, _ pinned: Bool) async throws {
		let body = try JSONSerialization.data(withJSONObject: ["pinned": pinned])
		_ = try await request("PATCH", "/api/memory/\(idTail(id))", body: body)
	}

	func forget(_ id: String) async throws {
		_ = try await request("DELETE", "/api/memory/\(idTail(id))")
	}

	// --- Entities ---
	func listEntities(limit: Int = 200) async throws -> [EntityLite] {
		let data = try await request("GET", "/api/entities?sort=mentions&limit=\(limit)")
		return try JSONDecoder().decode(EntitiesResponse.self, from: data).entities
	}

	// --- Edges ---
	func addEdge(_ edge: EdgeDecl) async throws {
		_ = try await request("POST", "/api/edge", body: try JSONEncoder().encode(edge))
	}

	// --- Settings (admin scope) ---
	func getSettingsMap() async throws -> [String: String] {
		let data = try await request("GET", "/api/settings")
		struct S: Codable { let key: String; let value: String? }
		struct Resp: Codable { let settings: [S] }
		let r = try JSONDecoder().decode(Resp.self, from: data)
		var out: [String: String] = [:]
		for s in r.settings { if let v = s.value, !v.isEmpty { out[s.key] = v } }
		return out
	}

	func updateSetting(_ key: String, _ value: String) async throws {
		let body = try JSONSerialization.data(withJSONObject: ["key": key, "value": value])
		_ = try await request("PATCH", "/api/settings", body: body)
	}

	// The REST router strips/normalizes a "memory:" prefix; pass the tail so the
	// path stays clean whether the id arrives as "memory:abc" or "abc".
	private func idTail(_ id: String) -> String {
		let raw = id.hasPrefix("memory:") ? String(id.dropFirst("memory:".count)) : id
		return raw.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? raw
	}
}
