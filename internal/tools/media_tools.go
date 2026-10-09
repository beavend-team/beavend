package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/guardrails"
	"github.com/beavend/beavend/internal/media"
)

// ModelAdapterResolver resolves a core.ProviderAdapter (and Generator) for a logical model name.
type ModelAdapterResolver interface {
	ResolveGenerator(ctx context.Context, logicalModel string, snap *core.Snapshot) (core.Generator, string, error)
}

// MediaToolExecutorConfig configures MediaToolExecutor.
type MediaToolExecutorConfig struct {
	MediaStore   core.MediaStore
	JobStore     media.JobStore
	Resolver     ModelAdapterResolver
	Guardrail    *guardrails.MediaGuardrail
	SSRFClient   *http.Client
	Snapshot     *core.Snapshot
	LogicalModel string // Default target logical model alias
	ProjectID    string
	Environment  string
	AIAppID      string
}

// MediaToolExecutor implements core.Executor for built-in generate_image, generate_speech, and generate_video tools.
type MediaToolExecutor struct {
	cfg MediaToolExecutorConfig
}

// NewMediaToolExecutor constructs a new MediaToolExecutor.
func NewMediaToolExecutor(cfg MediaToolExecutorConfig) *MediaToolExecutor {
	if cfg.SSRFClient == nil {
		cfg.SSRFClient = NewSafeHTTPClient(SafeDialerConfig{}, 30*time.Second)
	}
	if cfg.Guardrail == nil {
		cfg.Guardrail = guardrails.NewMediaGuardrail(guardrails.MediaGuardrailConfig{})
	}
	return &MediaToolExecutor{cfg: cfg}
}

// BuiltinImageToolDefinition returns the ToolDefinition for generate_image.
func BuiltinImageToolDefinition(targetModel string) core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "generate_image",
		Name:        "generate_image",
		Description: "Generate an image using a text description prompt. Returns a signed URL to the generated image.",
		SideEffect:  "write",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "A detailed description of the image to generate.",
				},
				"size": map[string]any{
					"type":        "string",
					"description": "Image dimensions, e.g. '1024x1024', '1792x1024', '1024x1792'.",
					"default":     "1024x1024",
				},
				"quality": map[string]any{
					"type":        "string",
					"description": "Image quality: 'standard' or 'hd'.",
					"default":     "standard",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Optional logical model override.",
				},
			},
			"required": []any{"prompt"},
		},
		Confirmation: "none",
		Executor: core.ExecutorConfig{
			Type:     "media_generate",
			Template: targetModel,
		},
	}
}

// BuiltinSpeechToolDefinition returns the ToolDefinition for generate_speech.
func BuiltinSpeechToolDefinition(targetModel string) core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "generate_speech",
		Name:        "generate_speech",
		Description: "Synthesize spoken audio from text input. Returns a signed URL to the generated audio.",
		SideEffect:  "write",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The text content to speak.",
				},
				"voice": map[string]any{
					"type":        "string",
					"description": "Voice identity, e.g. 'alloy', 'echo', 'fable', 'onyx', 'nova', 'shimmer'.",
					"default":     "alloy",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Audio container format: 'mp3', 'opus', 'aac', 'flac', 'wav'.",
					"default":     "mp3",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Optional logical model override.",
				},
			},
			"required": []any{"text"},
		},
		Confirmation: "none",
		Executor: core.ExecutorConfig{
			Type:     "media_generate",
			Template: targetModel,
		},
	}
}

// BuiltinVideoToolDefinition returns the ToolDefinition for generate_video.
func BuiltinVideoToolDefinition(targetModel string) core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "generate_video",
		Name:        "generate_video",
		Description: "Start asynchronous generation of a video from a text description prompt. Returns a job ID and status.",
		SideEffect:  "write",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "A detailed description of the video to create.",
				},
				"duration": map[string]any{
					"type":        "number",
					"description": "Video length in seconds (e.g. 5.0, 10.0).",
					"default":     5.0,
				},
				"size": map[string]any{
					"type":        "string",
					"description": "Video resolution, e.g. '1280x720', '1920x1080'.",
				},
				"webhook_url": map[string]any{
					"type":        "string",
					"description": "Optional webhook URL to receive job completion callbacks.",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Optional logical model override.",
				},
			},
			"required": []any{"prompt"},
		},
		Confirmation: "none",
		Executor: core.ExecutorConfig{
			Type:     "media_generate",
			Template: targetModel,
		},
	}
}

// Execute executes media generation tool calls (generate_image, generate_speech, generate_video).
func (e *MediaToolExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"request cancelled: %v"}`, err),
			IsError: true,
		}, nil
	}

	switch call.Name {
	case "generate_image":
		return e.executeImage(ctx, call)
	case "generate_speech":
		return e.executeSpeech(ctx, call)
	case "generate_video":
		return e.executeVideo(ctx, call)
	default:
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"unknown media tool %q"}`, call.Name),
			IsError: true,
		}, nil
	}
}

func (e *MediaToolExecutor) executeImage(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
	var args struct {
		Prompt  string `json:"prompt"`
		Size    string `json:"size"`
		Quality string `json:"quality"`
		Model   string `json:"model"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"failed to parse arguments: %v"}`, err),
			IsError: true,
		}, nil
	}

	targetModel := args.Model
	if targetModel == "" {
		targetModel = e.cfg.LogicalModel
	}
	if targetModel == "" {
		targetModel = "image"
	}

	generator, deployment, err := e.cfg.Resolver.ResolveGenerator(ctx, targetModel, e.cfg.Snapshot)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"resolve generator: %v"}`, err),
			IsError: true,
		}, nil
	}

	genReq := core.GenerateRequest{
		Model:    deployment,
		Modality: "image",
		Prompt:   args.Prompt,
		Size:     args.Size,
		Quality:  args.Quality,
		Count:    1,
	}

	genResp, err := generator.Generate(ctx, genReq)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"image generation failed: %v"}`, err),
			IsError: true,
		}, nil
	}

	if len(genResp.Items) == 0 {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: `{"status":"error","message":"no image generated"}`,
			IsError: true,
		}, nil
	}

	item := genResp.Items[0]

	// Output guardrail validation
	if e.cfg.Guardrail != nil {
		if err := e.cfg.Guardrail.CheckGeneratedItem(ctx, &item, 0); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  call.Name,
				Content: fmt.Sprintf(`{"status":"error","message":"guardrail rejected generated image: %v"}`, err),
				IsError: true,
			}, nil
		}
	}

	// Persist to MediaStore and obtain signed URL
	signedURL, err := e.persistAndSignMedia(ctx, item, "image/png", "generated_image.png")
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"persist media: %v"}`, err),
			IsError: true,
		}, nil
	}

	out := map[string]any{
		"status":         "success",
		"url":            signedURL,
		"revised_prompt": item.RevisedPrompt,
		"model":          targetModel,
	}
	payload, _ := json.Marshal(out)
	return core.ToolResult{
		CallID:   call.ID,
		ToolID:   call.Name,
		Content:  string(payload),
		IsError:  false,
		Metadata: map[string]any{"url": signedURL, "modality": "image"},
	}, nil
}

func (e *MediaToolExecutor) executeSpeech(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
	var args struct {
		Text   string `json:"text"`
		Voice  string `json:"voice"`
		Format string `json:"format"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"failed to parse arguments: %v"}`, err),
			IsError: true,
		}, nil
	}

	targetModel := args.Model
	if targetModel == "" {
		targetModel = e.cfg.LogicalModel
	}
	if targetModel == "" {
		targetModel = "speech"
	}

	generator, deployment, err := e.cfg.Resolver.ResolveGenerator(ctx, targetModel, e.cfg.Snapshot)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"resolve generator: %v"}`, err),
			IsError: true,
		}, nil
	}

	genReq := core.GenerateRequest{
		Model:    deployment,
		Modality: "audio",
		Prompt:   args.Text,
		Voice:    args.Voice,
		Format:   args.Format,
	}

	genResp, err := generator.Generate(ctx, genReq)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"speech synthesis failed: %v"}`, err),
			IsError: true,
		}, nil
	}

	if len(genResp.Items) == 0 {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: `{"status":"error","message":"no audio generated"}`,
			IsError: true,
		}, nil
	}

	item := genResp.Items[0]

	// Output guardrail validation
	if e.cfg.Guardrail != nil {
		if err := e.cfg.Guardrail.CheckGeneratedItem(ctx, &item, genResp.Usage.AudioSeconds); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  call.Name,
				Content: fmt.Sprintf(`{"status":"error","message":"guardrail rejected generated audio: %v"}`, err),
				IsError: true,
			}, nil
		}
	}

	mimeType := item.MimeType
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}
	filename := "speech.mp3"
	if strings.Contains(mimeType, "wav") {
		filename = "speech.wav"
	}

	signedURL, err := e.persistAndSignMedia(ctx, item, mimeType, filename)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"persist media: %v"}`, err),
			IsError: true,
		}, nil
	}

	out := map[string]any{
		"status":        "success",
		"url":           signedURL,
		"duration_secs": genResp.Usage.AudioSeconds,
		"model":         targetModel,
	}
	payload, _ := json.Marshal(out)
	return core.ToolResult{
		CallID:   call.ID,
		ToolID:   call.Name,
		Content:  string(payload),
		IsError:  false,
		Metadata: map[string]any{"url": signedURL, "modality": "audio"},
	}, nil
}

func (e *MediaToolExecutor) executeVideo(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
	var args struct {
		Prompt     string  `json:"prompt"`
		Duration   float64 `json:"duration"`
		Size       string  `json:"size"`
		WebhookURL string  `json:"webhook_url"`
		Model      string  `json:"model"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"failed to parse arguments: %v"}`, err),
			IsError: true,
		}, nil
	}

	targetModel := args.Model
	if targetModel == "" {
		targetModel = e.cfg.LogicalModel
	}
	if targetModel == "" {
		targetModel = "video"
	}

	// Create asynchronous media job
	jobID := media.GenerateJobID()
	job := &media.MediaJob{
		ID:          jobID,
		ProjectID:   e.cfg.ProjectID,
		Environment: e.cfg.Environment,
		AIAppID:     e.cfg.AIAppID,
		Model:       targetModel,
		Modality:    "video",
		Prompt:      args.Prompt,
		Status:      "queued",
		WebhookURL:  args.WebhookURL,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if e.cfg.JobStore != nil {
		if err := e.cfg.JobStore.CreateJob(ctx, job, 24*time.Hour); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  call.Name,
				Content: fmt.Sprintf(`{"status":"error","message":"create job: %v"}`, err),
				IsError: true,
			}, nil
		}
	}

	// Kick off background job execution
	go e.runAsyncVideoJob(job, args.Duration, args.Size)

	out := map[string]any{
		"status":      "queued",
		"job_id":      jobID,
		"poll_url":    fmt.Sprintf("/v1/apps/%s/jobs/%s", e.cfg.AIAppID, jobID),
		"webhook_url": args.WebhookURL,
		"model":       targetModel,
		"message":     "Video generation started in the background. Use the job ID to poll for status.",
	}
	payload, _ := json.Marshal(out)
	return core.ToolResult{
		CallID:   call.ID,
		ToolID:   call.Name,
		Content:  string(payload),
		IsError:  false,
		Metadata: map[string]any{"job_id": jobID, "modality": "video"},
	}, nil
}

func (e *MediaToolExecutor) runAsyncVideoJob(job *media.MediaJob, duration float64, size string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	generator, deployment, err := e.cfg.Resolver.ResolveGenerator(bgCtx, job.Model, e.cfg.Snapshot)
	if err != nil {
		e.failJob(bgCtx, job, fmt.Sprintf("resolve generator: %v", err))
		return
	}

	job.Status = "processing"
	job.UpdatedAt = time.Now().UTC()
	if e.cfg.JobStore != nil {
		_ = e.cfg.JobStore.UpdateJob(bgCtx, job, 24*time.Hour)
	}

	genReq := core.GenerateRequest{
		Model:    deployment,
		Modality: "video",
		Prompt:   job.Prompt,
		Duration: duration,
		Size:     size,
	}

	genResp, err := generator.Generate(bgCtx, genReq)
	if err != nil {
		e.failJob(bgCtx, job, fmt.Sprintf("generate video: %v", err))
		return
	}

	if len(genResp.Items) > 0 {
		item := genResp.Items[0]
		// Output guardrail validation
		if e.cfg.Guardrail != nil {
			if gErr := e.cfg.Guardrail.CheckGeneratedItem(bgCtx, &item, genResp.Usage.VideoSeconds); gErr != nil {
				e.failJob(bgCtx, job, fmt.Sprintf("guardrail violation: %v", gErr))
				return
			}
		}

		signedURL, pErr := e.persistAndSignMedia(bgCtx, item, "video/mp4", "video.mp4")
		if pErr == nil && signedURL != "" {
			item.URL = signedURL
			genResp.Items[0] = item
		}
	}

	job.Status = "completed"
	job.Result = genResp
	job.UpdatedAt = time.Now().UTC()
	if e.cfg.JobStore != nil {
		_ = e.cfg.JobStore.UpdateJob(bgCtx, job, 24*time.Hour)
	}

	// Trigger optional webhook if configured
	if job.WebhookURL != "" {
		e.dispatchWebhook(bgCtx, job)
	}
}

func (e *MediaToolExecutor) failJob(ctx context.Context, job *media.MediaJob, errMsg string) {
	job.Status = "failed"
	job.Error = errMsg
	job.UpdatedAt = time.Now().UTC()
	if e.cfg.JobStore != nil {
		_ = e.cfg.JobStore.UpdateJob(ctx, job, 24*time.Hour)
	}
	if job.WebhookURL != "" {
		e.dispatchWebhook(ctx, job)
	}
}

func (e *MediaToolExecutor) dispatchWebhook(ctx context.Context, job *media.MediaJob) {
	payload, err := json.Marshal(job)
	if err != nil {
		return
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, job.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Beavend-Media-Worker/1.0")

	// Use SSRF-safe client for outbound webhook dispatch
	resp, err := e.cfg.SSRFClient.Do(httpReq)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func (e *MediaToolExecutor) persistAndSignMedia(ctx context.Context, item core.GeneratedItem, defaultMime, filename string) (string, error) {
	if e.cfg.MediaStore == nil {
		// No media store configured: return upstream URL directly
		if item.URL != "" {
			return item.URL, nil
		}
		return "", errors.New("no media store configured and no upstream URL returned")
	}

	projectID := e.cfg.ProjectID
	if projectID == "" {
		projectID = "default"
	}

	var reader io.Reader
	mimeType := item.MimeType
	if mimeType == "" {
		mimeType = defaultMime
	}

	if len(item.Data) > 0 {
		reader = bytes.NewReader(item.Data)
	} else if item.URL != "" {
		// Fetch remote media payload via SSRF-safe client
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
		if err != nil {
			return "", fmt.Errorf("create fetch request: %w", err)
		}
		resp, err := e.cfg.SSRFClient.Do(httpReq)
		if err != nil {
			return "", fmt.Errorf("fetch remote media: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("fetch remote media returned status %d", resp.StatusCode)
		}

		if ct := resp.Header.Get("Content-Type"); ct != "" && mimeType == defaultMime {
			mimeType = strings.Split(ct, ";")[0]
		}

		// Buffer into memory (up to 50MB)
		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, io.LimitReader(resp.Body, 50<<20)); err != nil {
			return "", fmt.Errorf("read remote media bytes: %w", err)
		}
		reader = buf
	} else {
		return "", errors.New("neither media data nor URL provided")
	}

	mediaRef, err := e.cfg.MediaStore.Put(ctx, projectID, reader, mimeType, filename)
	if err != nil {
		return "", fmt.Errorf("mediastore put: %w", err)
	}

	return mediaRef.URL, nil
}
