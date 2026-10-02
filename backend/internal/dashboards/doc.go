// Package dashboards stores user-built dashboards, decides who may see and
// change them, and resolves each widget's data. It sits above services: it
// calls them, and nothing in services imports it.
//
// Spec: docs/superpowers/specs/2026-10-02-network-phase4-dashboards-design.md
package dashboards
