package sync

// RevisionEvent represents a configuration revision change broadcast across runtime instances.
type RevisionEvent struct {
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment"`
	Revision    int64  `json:"revision"`
}

// ChannelForProject returns the Redis pub/sub channel name for a project.
func ChannelForProject(projectID string) string {
	return "cfg:" + projectID
}
