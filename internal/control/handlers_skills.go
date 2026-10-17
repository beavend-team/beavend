package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/prompts"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/beavend/beavend/internal/tools"
	chi "github.com/go-chi/chi/v5"
)

const maxSkillMarkdownBytes = 512 * 1024 // 512 KB cap for imported SKILL.md files

var skillVarRegex = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
var nonSlugRegex = regexp.MustCompile(`[^a-z0-9_-]+`)

type CreateSkillRequest struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Source        string          `json:"source"`
	InjectionMode string          `json:"injection_mode"`
	Content       string          `json:"content"`
	RawMarkdown   string          `json:"raw_markdown"`
	Variables     json.RawMessage `json:"variables"`
}

type UpdateSkillRequest struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Source        string          `json:"source"`
	InjectionMode string          `json:"injection_mode"`
	Content       string          `json:"content"`
	RawMarkdown   string          `json:"raw_markdown"`
	Variables     json.RawMessage `json:"variables"`
}

type CreateSkillVersionRequest struct {
	Content   string          `json:"content"`
	Variables json.RawMessage `json:"variables"`
	SetActive bool            `json:"set_active"`
}

type SkillDetailsResponse struct {
	Skill         db.Skills         `json:"skill"`
	ActiveVersion *db.SkillVersions `json:"active_version,omitempty"`
	Versions      []db.SkillVersions `json:"versions"`
}

type ImportSkillRequest struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	InjectionMode string `json:"injection_mode"`
	Save          bool   `json:"save"`
}

type ImportSkillResponse struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Source        string            `json:"source"`
	ResolvedURL   string            `json:"resolved_url"`
	InjectionMode string            `json:"injection_mode"`
	Content       string            `json:"content"`
	RawMarkdown   string            `json:"raw_markdown"`
	Metadata      map[string]any    `json:"metadata"`
	Variables     []string          `json:"variables"`
	Saved         bool              `json:"saved"`
	Skill         *db.Skills        `json:"skill,omitempty"`
	ActiveVersion *db.SkillVersions `json:"active_version,omitempty"`
}

func extractTemplateVariables(text string) []string {
	matches := skillVarRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var vars []string
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			vars = append(vars, m[1])
		}
	}
	if vars == nil {
		return []string{}
	}
	return vars
}

func slugifySkillID(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonSlugRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if s == "" {
		return "skill"
	}
	return s
}

func normalizeInjectionMode(mode string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return core.SkillInjectionAlways, nil
	}
	if m != core.SkillInjectionAlways && m != core.SkillInjectionOnDemand {
		return "", fmt.Errorf("invalid injection_mode %q; must be %q or %q", mode, core.SkillInjectionAlways, core.SkillInjectionOnDemand)
	}
	return m, nil
}

func (h *Handler) HandleListSkills(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	skills, err := h.svc.queries.ListSkills(r.Context(), db.ListSkillsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, skills)
}

func (h *Handler) HandleCreateSkill(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	var req CreateSkillRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// If RawMarkdown is provided, parse frontmatter to extract name, description, and instructions
	content := req.Content
	if strings.TrimSpace(req.RawMarkdown) != "" {
		parsed := prompts.ParseSkillMarkdown(req.RawMarkdown)
		if strings.TrimSpace(content) == "" {
			content = parsed.Body
		}
		if strings.TrimSpace(req.Name) == "" {
			req.Name = parsed.Name
		}
		if strings.TrimSpace(req.Description) == "" {
			req.Description = parsed.Description
		}
		if strings.TrimSpace(req.InjectionMode) == "" && parsed.InjectionMode != "" {
			req.InjectionMode = parsed.InjectionMode
		}
	} else if strings.TrimSpace(content) != "" && strings.TrimSpace(req.Name) == "" {
		parsed := prompts.ParseSkillMarkdown(content)
		if strings.TrimSpace(req.Name) == "" {
			req.Name = parsed.Name
		}
		if strings.TrimSpace(req.Description) == "" {
			req.Description = parsed.Description
		}
		if strings.TrimSpace(req.InjectionMode) == "" && parsed.InjectionMode != "" {
			req.InjectionMode = parsed.InjectionMode
		}
		content = parsed.Body
	}

	req.Name = strings.TrimSpace(req.Name)
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" && req.Name != "" {
		req.ID = slugifySkillID(req.Name)
	}
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id and name are required")
		return
	}

	mode, err := normalizeInjectionMode(req.InjectionMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	req.InjectionMode = mode

	if strings.TrimSpace(req.Source) == "" {
		req.Source = "manual"
	}
	if len(req.Variables) == 0 {
		vars := extractTemplateVariables(content)
		req.Variables, _ = json.Marshal(vars)
	}

	skill, err := h.svc.queries.CreateSkill(r.Context(), db.CreateSkillParams{
		ID:            req.ID,
		ProjectID:     projectID,
		Environment:   env,
		Name:          req.Name,
		Description:   req.Description,
		Source:        req.Source,
		InjectionMode: req.InjectionMode,
		ActiveVersion: 1,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}

	versionID := fmt.Sprintf("%s_v1", req.ID)
	_, err = h.svc.queries.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
		ID:          versionID,
		SkillID:     req.ID,
		ProjectID:   projectID,
		Environment: env,
		Version:     1,
		Content:     content,
		Variables:   req.Variables,
		IsActive:    true,
	})
	if err != nil {
		_ = h.svc.queries.DeleteSkill(r.Context(), req.ID)
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_skill", "skill", skill.ID, req)

	writeJSON(w, http.StatusCreated, skill)
}

func (h *Handler) HandleGetSkill(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	skillID := chi.URLParam(r, "id")
	skill, err := h.svc.queries.GetSkill(r.Context(), skillID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return
	}

	versions, err := h.svc.queries.ListSkillVersions(r.Context(), skillID)
	if err != nil {
		versions = []db.SkillVersions{}
	}

	var activePtr *db.SkillVersions
	activeVer, err := h.svc.queries.GetActiveSkillVersion(r.Context(), skillID)
	if err == nil {
		activePtr = &activeVer
	} else if len(versions) > 0 {
		activePtr = &versions[0]
	}

	writeJSON(w, http.StatusOK, SkillDetailsResponse{
		Skill:         skill,
		ActiveVersion: activePtr,
		Versions:      versions,
	})
}

func (h *Handler) HandleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	skillID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req UpdateSkillRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := h.svc.queries.GetSkill(r.Context(), skillID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return
	}

	content := req.Content
	if strings.TrimSpace(req.RawMarkdown) != "" {
		parsed := prompts.ParseSkillMarkdown(req.RawMarkdown)
		if strings.TrimSpace(content) == "" {
			content = parsed.Body
		}
		if strings.TrimSpace(req.Name) == "" && parsed.Name != "" {
			req.Name = parsed.Name
		}
		if strings.TrimSpace(req.Description) == "" && parsed.Description != "" {
			req.Description = parsed.Description
		}
		if strings.TrimSpace(req.InjectionMode) == "" && parsed.InjectionMode != "" {
			req.InjectionMode = parsed.InjectionMode
		}
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	desc := req.Description
	if desc == "" {
		desc = existing.Description
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = existing.Source
	}

	modeStr := req.InjectionMode
	if strings.TrimSpace(modeStr) == "" {
		modeStr = existing.InjectionMode
	}
	mode, err := normalizeInjectionMode(modeStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}

	activeVer := existing.ActiveVersion
	if strings.TrimSpace(content) != "" {
		variables := req.Variables
		if len(variables) == 0 {
			vars := extractTemplateVariables(content)
			variables, _ = json.Marshal(vars)
		}

		newVersion := existing.ActiveVersion + 1
		versionID := fmt.Sprintf("%s_v%d", skillID, newVersion)

		_, err = h.svc.queries.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
			ID:          versionID,
			SkillID:     skillID,
			ProjectID:   projectID,
			Environment: env,
			Version:     newVersion,
			Content:     content,
			Variables:   variables,
			IsActive:    true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database_error", err.Error())
			return
		}
		activeVer = newVersion
	}

	updatedSkill, err := h.svc.queries.UpdateSkill(r.Context(), db.UpdateSkillParams{
		ID:            skillID,
		Name:          name,
		Description:   desc,
		Source:        source,
		InjectionMode: mode,
		ActiveVersion: activeVer,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "update_skill", "skill", skillID, req)

	writeJSON(w, http.StatusOK, updatedSkill)
}

func (h *Handler) HandleCreateSkillVersion(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	skillID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req CreateSkillVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := h.svc.queries.GetSkill(r.Context(), skillID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return
	}

	content := req.Content
	if strings.TrimSpace(content) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "content is required")
		return
	}

	newVersion := existing.ActiveVersion + 1
	versionID := fmt.Sprintf("%s_v%d", skillID, newVersion)
	if len(req.Variables) == 0 {
		vars := extractTemplateVariables(content)
		req.Variables, _ = json.Marshal(vars)
	}

	createdVersion, err := h.svc.queries.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
		ID:          versionID,
		SkillID:     skillID,
		ProjectID:   projectID,
		Environment: env,
		Version:     newVersion,
		Content:     content,
		Variables:   req.Variables,
		IsActive:    req.SetActive,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	if req.SetActive {
		_, _ = h.svc.queries.UpdateSkillActiveVersion(r.Context(), db.UpdateSkillActiveVersionParams{
			ID:            skillID,
			ActiveVersion: newVersion,
		})
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_skill_version", "skill", skillID, req)

	writeJSON(w, http.StatusCreated, createdVersion)
}

func (h *Handler) HandleSetActiveSkillVersion(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	skillID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req SetActiveVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "valid version is required")
		return
	}

	_, err := h.svc.queries.GetSkillVersion(r.Context(), db.GetSkillVersionParams{
		SkillID: skillID,
		Version: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill version not found")
		return
	}

	updated, err := h.svc.queries.UpdateSkillActiveVersion(r.Context(), db.UpdateSkillActiveVersionParams{
		ID:            skillID,
		ActiveVersion: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "set_active_skill_version", "skill", skillID, req)

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) HandleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	skillID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeleteSkill(r.Context(), skillID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_skill", "skill", skillID, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": skillID})
}

// ResolveSkillSourceCandidateURLs parses a skill source string (npx skills add command,
// skills.sh URL, GitHub tree/blob URL, owner/repo/skill shorthand, or raw URL)
// and returns the detected normalized source identifier and candidate raw SKILL.md URLs.
func ResolveSkillSourceCandidateURLs(source string) (sourceRef string, candidateURLs []string, err error) {
	raw := strings.TrimSpace(source)
	if raw == "" {
		return "", nil, fmt.Errorf("source is required")
	}

	// 1. Check for `npx skills add ...` or `skills add ...` CLI command
	if strings.HasPrefix(raw, "npx ") || strings.HasPrefix(raw, "pnpm dlx ") || strings.HasPrefix(raw, "bunx ") || strings.HasPrefix(raw, "skills add ") {
		tokens := strings.Fields(raw)
		var repoArg, skillFlag string
		for i := 0; i < len(tokens); i++ {
			tok := tokens[i]
			if tok == "npx" || tok == "pnpm" || tok == "dlx" || tok == "bunx" || tok == "skills" || tok == "add" || tok == "-y" {
				continue
			}
			if (tok == "--skill" || tok == "-s") && i+1 < len(tokens) {
				skillFlag = strings.Trim(tokens[i+1], `"'`)
				i++
				continue
			}
			if strings.HasPrefix(tok, "--skill=") {
				skillFlag = strings.Trim(strings.TrimPrefix(tok, "--skill="), `"'`)
				continue
			}
			if strings.HasPrefix(tok, "-") {
				continue
			}
			if repoArg == "" {
				repoArg = strings.Trim(tok, `"'`)
			}
		}
		if repoArg == "" {
			return "", nil, fmt.Errorf("could not parse repository from skills CLI command")
		}
		if skillFlag != "" && !strings.HasSuffix(repoArg, "/"+skillFlag) {
			repoArg = strings.TrimSuffix(repoArg, "/") + "/" + skillFlag
		}
		_, urls, err := ResolveSkillSourceCandidateURLs(repoArg)
		if err != nil {
			return "", nil, err
		}
		return raw, urls, nil
	}

	// 2. Check if it is an HTTP/HTTPS URL or starts with skills.sh / github.com
	normalizedURL := raw
	if strings.HasPrefix(raw, "skills.sh/") || strings.HasPrefix(raw, "github.com/") || strings.HasPrefix(raw, "raw.githubusercontent.com/") {
		normalizedURL = "https://" + raw
	}

	if strings.HasPrefix(normalizedURL, "http://") || strings.HasPrefix(normalizedURL, "https://") {
		u, parseErr := url.Parse(normalizedURL)
		if parseErr != nil {
			return "", nil, fmt.Errorf("invalid source URL: %w", parseErr)
		}
		host := strings.ToLower(u.Hostname())
		trimmedPath := strings.Trim(u.Path, "/")
		parts := strings.Split(trimmedPath, "/")

		switch host {
		case "skills.sh", "www.skills.sh":
			if len(parts) < 2 {
				return "", nil, fmt.Errorf("skills.sh URL must include at least <owner>/<repo>")
			}
			owner, repo := parts[0], parts[1]
			skillName := ""
			if len(parts) >= 3 {
				skillName = strings.Join(parts[2:], "/")
			}
			return raw, buildGitHubSkillCandidates(owner, repo, skillName), nil

		case "github.com", "www.github.com":
			if len(parts) < 2 {
				return "", nil, fmt.Errorf("github.com URL must include <owner>/<repo>")
			}
			owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
			if len(parts) >= 5 && (parts[2] == "blob" || parts[2] == "raw") {
				branch := parts[3]
				filePath := strings.Join(parts[4:], "/")
				rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, filePath)
				return raw, []string{rawURL}, nil
			}
			if len(parts) >= 4 && parts[2] == "tree" {
				branch := parts[3]
				dirPath := strings.Join(parts[4:], "/")
				var urls []string
				if dirPath != "" {
					if strings.HasSuffix(strings.ToLower(dirPath), ".md") {
						urls = append(urls, fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, dirPath))
					} else {
						urls = append(urls, fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s/SKILL.md", owner, repo, branch, dirPath))
					}
				} else {
					urls = append(urls, fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/SKILL.md", owner, repo, branch))
				}
				return raw, urls, nil
			}
			skillName := ""
			if len(parts) >= 3 {
				skillName = strings.Join(parts[2:], "/")
			}
			return raw, buildGitHubSkillCandidates(owner, repo, skillName), nil

		default:
			return raw, []string{normalizedURL}, nil
		}
	}

	// 3. Shorthand `<owner>/<repo>` or `<owner>/<repo>/<skill>`
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) >= 2 {
		owner, repo := parts[0], parts[1]
		skillName := ""
		if len(parts) >= 3 {
			skillName = strings.Join(parts[2:], "/")
		}
		return raw, buildGitHubSkillCandidates(owner, repo, skillName), nil
	}

	return "", nil, fmt.Errorf("unrecognized skill source %q; expected skills.sh URL, GitHub URL, npx skills add command, or owner/repo/skill shorthand", source)
}

func buildGitHubSkillCandidates(owner, repo, skill string) []string {
	branches := []string{"main", "master"}
	var urls []string
	for _, b := range branches {
		if skill != "" {
			if strings.HasSuffix(strings.ToLower(skill), ".md") {
				urls = append(urls, fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, b, skill))
			}
			urls = append(urls,
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/skills/%s/SKILL.md", owner, repo, b, skill),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s/SKILL.md", owner, repo, b, skill),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/.claude/skills/%s/SKILL.md", owner, repo, b, skill),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/.agents/skills/%s/SKILL.md", owner, repo, b, skill),
			)
		} else {
			urls = append(urls,
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/SKILL.md", owner, repo, b),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/skills/%s/SKILL.md", owner, repo, b, repo),
			)
		}
	}
	return urls
}

func (h *Handler) fetchSkillMarkdownFromCandidates(ctx context.Context, candidateURLs []string) (string, string, error) {
	client := h.svc.SkillHTTPClient()
	var lastErr error

	for _, targetURL := range candidateURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "text/markdown, text/plain, */*")
		req.Header.Set("User-Agent", "Beavend-Skill-Importer/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxSkillMarkdownBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusOK && strings.TrimSpace(string(bodyBytes)) != "" {
			return string(bodyBytes), targetURL, nil
		}
		lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, targetURL)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate URLs resolved")
	}
	return "", "", lastErr
}

func (h *Handler) HandleImportSkill(w http.ResponseWriter, r *http.Request) {
	projectID, env := getProjectAndEnv(r)

	var req ImportSkillRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	mode, err := normalizeInjectionMode(req.InjectionMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}

	sourceRef, candidates, err := ResolveSkillSourceCandidateURLs(req.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_source", err.Error())
		return
	}

	rawMarkdown, resolvedURL, err := h.fetchSkillMarkdownFromCandidates(r.Context(), candidates)
	if err != nil {
		if errors.Is(err, tools.ErrBlockedHost) || errors.Is(err, tools.ErrBlockedIP) || errors.Is(err, tools.ErrNoSafeIPs) || strings.Contains(err.Error(), "ssrf:") {
			writeError(w, http.StatusForbidden, "ssrf_blocked", fmt.Sprintf("blocked unsafe skill URL: %v", err))
			return
		}
		writeError(w, http.StatusBadGateway, "fetch_failed", fmt.Sprintf("failed to fetch SKILL.md: %v", err))
		return
	}

	parsed := prompts.ParseSkillMarkdown(rawMarkdown)

	skillName := strings.TrimSpace(parsed.Name)
	if skillName == "" {
		parts := strings.Split(strings.Trim(sourceRef, "/"), "/")
		skillName = parts[len(parts)-1]
		skillName = strings.TrimSuffix(skillName, ".md")
		if skillName == "" || strings.EqualFold(skillName, "SKILL") {
			if len(parts) >= 2 {
				skillName = parts[len(parts)-2]
			} else {
				skillName = "imported-skill"
			}
		}
	}

	skillID := strings.TrimSpace(req.ID)
	if skillID == "" {
		skillID = slugifySkillID(skillName)
	}

	if req.InjectionMode == "" && parsed.InjectionMode != "" {
		if normMode, err := normalizeInjectionMode(parsed.InjectionMode); err == nil {
			mode = normMode
		}
	}

	vars := parsed.Variables
	if len(vars) == 0 {
		vars = extractTemplateVariables(parsed.Body)
	}

	resp := ImportSkillResponse{
		ID:            skillID,
		Name:          skillName,
		Description:   parsed.Description,
		Source:        sourceRef,
		ResolvedURL:   resolvedURL,
		InjectionMode: mode,
		Content:       parsed.Body,
		RawMarkdown:   rawMarkdown,
		Metadata:      map[string]any{},
		Variables:     vars,
		Saved:         false,
	}

	if req.Save {
		if h.svc.queries == nil {
			writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
			return
		}
		varBytes, _ := json.Marshal(vars)

		var skillRecord db.Skills
		var verRecord db.SkillVersions

		existing, getErr := h.svc.queries.GetSkill(r.Context(), skillID)
		if getErr == nil {
			newVerNum := existing.ActiveVersion + 1
			verID := fmt.Sprintf("%s_v%d", skillID, newVerNum)
			verRecord, err = h.svc.queries.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
				ID:          verID,
				SkillID:     skillID,
				ProjectID:   projectID,
				Environment: env,
				Version:     newVerNum,
				Content:     parsed.Body,
				Variables:   varBytes,
				IsActive:    true,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database_error", err.Error())
				return
			}
			skillRecord, err = h.svc.queries.UpdateSkill(r.Context(), db.UpdateSkillParams{
				ID:            skillID,
				Name:          skillName,
				Description:   parsed.Description,
				Source:        sourceRef,
				InjectionMode: mode,
				ActiveVersion: newVerNum,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database_error", err.Error())
				return
			}
		} else {
			skillRecord, err = h.svc.queries.CreateSkill(r.Context(), db.CreateSkillParams{
				ID:            skillID,
				ProjectID:     projectID,
				Environment:   env,
				Name:          skillName,
				Description:   parsed.Description,
				Source:        sourceRef,
				InjectionMode: mode,
				ActiveVersion: 1,
			})
			if err != nil {
				writeError(w, http.StatusConflict, "conflict", err.Error())
				return
			}
			verID := fmt.Sprintf("%s_v1", skillID)
			verRecord, err = h.svc.queries.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
				ID:          verID,
				SkillID:     skillID,
				ProjectID:   projectID,
				Environment: env,
				Version:     1,
				Content:     parsed.Body,
				Variables:   varBytes,
				IsActive:    true,
			})
			if err != nil {
				_ = h.svc.queries.DeleteSkill(r.Context(), skillID)
				writeError(w, http.StatusInternalServerError, "database_error", err.Error())
				return
			}
		}

		_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
		h.svc.RecordAudit(r.Context(), projectID, env, "import_skill", "skill", skillID, req)

		resp.Saved = true
		resp.Skill = &skillRecord
		resp.ActiveVersion = &verRecord
	}

	status := http.StatusOK
	if resp.Saved {
		status = http.StatusCreated
	}
	writeJSON(w, status, resp)
}
