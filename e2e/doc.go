// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Package documentation for the e2e suite; the only file built
//   without the e2e tag, so tools that skip build tags still see a package.
// Author review: James Teo — reviewed and verified.

// Package e2e drives the whole Compose stack through the API Gateway, the way
// a browser does, with real tokens. Every test is behind the e2e build
// tag; README.md has the command that starts the stack and runs the suite.
package e2e
