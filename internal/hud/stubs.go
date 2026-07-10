package hud

import "net/http"

// Handlers stubbed until their task lands. Task 6 removes handleRuns;
// Task 7 removes handleLibrary + handleModels; Task 8 removes the last three
// and this file with them.
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
