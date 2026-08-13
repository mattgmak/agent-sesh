package picker

import "github.com/mattgmak/agent-sesh/internal/prof"

// Thin wrappers over internal/prof so the picker keeps its local naming.

func initProfile() (string, error) { return prof.Init() }
func closeProfile() string         { return prof.Close() }

func profileStart(name string) func() { return prof.Start(name) }
func profileNote(name, detail string) { prof.Note(name, detail) }

func startCPUProfile() (string, error) { return prof.StartCPUProfile() }
func stopCPUProfile() string           { return prof.StopCPUProfile() }
