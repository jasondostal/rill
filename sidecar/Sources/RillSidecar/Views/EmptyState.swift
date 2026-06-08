// EmptyState.swift — first-run hero when there are no memories.

import SwiftUI

struct EmptyState: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState

	private let examples = [
		("Prefer dark-first, keyboard-driven UIs", "preference"),
		("Look into SurrealDB live queries for the sidecar", "idea"),
		("Shipped the v1 release today", "decision"),
	]

	var body: some View {
		let t = theme.tokens
		VStack(spacing: 10) {
			DropletsShape()
				.stroke(t.accent, style: StrokeStyle(lineWidth: 1.8, lineCap: .round, lineJoin: .round))
				.frame(width: 32, height: 32)
				.opacity(0.9)
			Text("No memories yet").font(.system(size: 15, weight: .semibold)).foregroundColor(t.text)
			Text("Catch your first thought above — a decision, a fact, or something to look into.")
				.font(.system(size: 12.5)).foregroundColor(t.textDim)
				.multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
				.frame(maxWidth: 260)

			VStack(spacing: 6) {
				ForEach(examples, id: \.0) { ex in
					Button {
						state.draftText = ex.0
						state.draftKind = ex.1
					} label: {
						HStack(spacing: 6) {
							Circle().fill(t.kind(ex.1)).frame(width: 6, height: 6)
							Text("try “\(ex.0)”").font(.system(size: 11.5)).foregroundColor(t.textDim)
								.lineLimit(1)
						}
						.padding(.horizontal, 10).padding(.vertical, 6)
						.frame(maxWidth: .infinity, alignment: .leading)
						.background(RoundedRectangle(cornerRadius: 6).fill(t.surface))
						.overlay(RoundedRectangle(cornerRadius: 6).stroke(t.border, lineWidth: 1))
					}
					.buttonStyle(.plain)
				}
			}
			.padding(.top, 4)

			Mono("type @ to link a person or project", size: 10, color: t.textFaint).padding(.top, 2)
		}
		.frame(maxWidth: .infinity).padding(.vertical, 22)
	}
}
