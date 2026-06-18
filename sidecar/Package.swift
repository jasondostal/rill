// swift-tools-version:5.9
import PackageDescription

let package = Package(
	name: "RillSidecar",
	platforms: [.macOS(.v13)],
	targets: [
		.executableTarget(
			name: "RillSidecar",
			path: "Sources/RillSidecar"
		)
	]
)
