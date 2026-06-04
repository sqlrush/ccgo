package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestPromptMessagesBuildsImageBlocksAndMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	image := PastedContent{ID: 2, Type: PastedContentImage, Content: "AAAA", MediaType: "image/png", Filename: "chart.png"}
	if _, ok := CacheImagePath("session-1", image); !ok {
		t.Fatal("failed to cache image path")
	}
	messages := PromptMessages("look [Pasted text #1] [Image #2]", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "expanded paste"},
		2: image,
	})
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	user := messages[0]
	if user.Type != contracts.MessageUser || user.IsMeta || len(user.Content) != 2 {
		t.Fatalf("user message = %#v", user)
	}
	if user.Content[0].Type != contracts.ContentText || user.Content[0].Text != "look expanded paste [Image #2]" {
		t.Fatalf("text block = %#v", user.Content[0])
	}
	if user.Content[1].Type != contracts.ContentImage {
		t.Fatalf("image block = %#v", user.Content[1])
	}
	source, ok := user.Content[1].Source.(contracts.ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "AAAA" {
		t.Fatalf("image source = %#v", user.Content[1].Source)
	}
	encoded, err := json.Marshal(contracts.APIMessage{Role: "user", Content: user.Content})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"source":{"type":"base64","media_type":"image/png","data":"AAAA"}`) {
		t.Fatalf("encoded image block = %s", encoded)
	}
	if strings.Contains(string(encoded), `"content":{"type":"base64"`) {
		t.Fatalf("image source encoded as content: %s", encoded)
	}

	metadata := messages[1]
	wantPath := filepath.Join(dir, "image-cache", "session-1", "2.png")
	if !metadata.IsMeta || promptMessageText(metadata) != "[Image source: "+wantPath+"]" {
		t.Fatalf("metadata = %#v text=%q", metadata, promptMessageText(metadata))
	}
}

func TestPromptMessagesUsesImageDimensionsMetadata(t *testing.T) {
	messages := PromptMessages("[Image #1]", map[int]PastedContent{
		1: {
			ID:         1,
			Type:       PastedContentImage,
			Content:    "AAAA",
			MediaType:  "image/png",
			SourcePath: "/tmp/source.png",
			Dimensions: &ImageDimensions{
				OriginalWidth:  4000,
				OriginalHeight: 2000,
				DisplayWidth:   1000,
				DisplayHeight:  500,
			},
		},
	})
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if got := promptMessageText(messages[1]); !strings.Contains(got, "source: /tmp/source.png") || !strings.Contains(got, "Multiply coordinates by 4.00") {
		t.Fatalf("metadata = %q", got)
	}
}

func TestPromptMessagesFiltersUnreferencedImages(t *testing.T) {
	messages := PromptMessages("look", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentImage, Content: "AAAA", MediaType: "image/png", SourcePath: "/tmp/orphan.png"},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	if got := promptMessageText(messages[0]); got != "look" || len(messages[0].Content) != 1 {
		t.Fatalf("message = %#v text=%q", messages[0], got)
	}

	messages = PromptMessages("look [pasted image #2]", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentImage, Content: "AAAA", MediaType: "image/png", SourcePath: "/tmp/orphan.png"},
		2: {ID: 2, Type: PastedContentImage, Content: "BBBB", MediaType: "image/jpeg"},
	})
	if len(messages) != 1 {
		t.Fatalf("referenced messages = %#v", messages)
	}
	if len(messages[0].Content) != 2 || messages[0].Content[1].Type != contracts.ContentImage {
		t.Fatalf("referenced message = %#v", messages[0])
	}
	source, ok := messages[0].Content[1].Source.(contracts.ImageSource)
	if !ok || source.Data != "BBBB" {
		t.Fatalf("referenced image source = %#v", messages[0].Content[1].Source)
	}
}

func TestPromptMessagesTextOnly(t *testing.T) {
	messages := PromptMessages("run [Pasted text #1]", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "expanded"},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	if promptMessageText(messages[0]) != "run expanded" {
		t.Fatalf("text = %q", promptMessageText(messages[0]))
	}
}

func TestImageMetadataTextUsesDimensionsAndSourcePath(t *testing.T) {
	got := ImageMetadataText(&ImageDimensions{
		OriginalWidth:  4000,
		OriginalHeight: 2000,
		DisplayWidth:   1000,
		DisplayHeight:  500,
	}, "/tmp/chart.png")
	want := "[Image: source: /tmp/chart.png, original 4000x2000, displayed at 1000x500. Multiply coordinates by 4.00 to map to original image.]"
	if got != want {
		t.Fatalf("metadata = %q, want %q", got, want)
	}

	if got := ImageMetadataText(&ImageDimensions{OriginalWidth: 10, OriginalHeight: 10, DisplayWidth: 10, DisplayHeight: 10}, ""); got != "" {
		t.Fatalf("unresized metadata = %q", got)
	}
	if got := ImageMetadataText(&ImageDimensions{}, "/tmp/source.png"); got != "[Image source: /tmp/source.png]" {
		t.Fatalf("source-only metadata = %q", got)
	}
}

func promptMessageText(message contracts.Message) string {
	parts := []string{}
	for _, block := range message.Content {
		if block.Type == contracts.ContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}
