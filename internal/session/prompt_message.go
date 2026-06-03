package session

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

func PromptMessage(display string, pastedContents map[int]PastedContent) contracts.Message {
	text := ExpandPastedTextRefs(display, pastedContents)
	imageBlocks := promptImageContentBlocks(pastedContents)
	blocks := []contracts.ContentBlock{}
	if len(imageBlocks) > 0 {
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, contracts.NewTextBlock(text))
		}
		blocks = append(blocks, imageBlocks...)
	} else {
		blocks = append(blocks, contracts.NewTextBlock(text))
	}
	return contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		Content: blocks,
	}
}

func PromptMessages(display string, pastedContents map[int]PastedContent) []contracts.Message {
	user := PromptMessage(display, pastedContents)
	metadataBlocks := promptImageMetadataBlocks(pastedContents)
	if len(metadataBlocks) == 0 {
		return []contracts.Message{user}
	}
	return []contracts.Message{user, contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		IsMeta:  true,
		Content: metadataBlocks,
	}}
}

func promptImageContentBlocks(pastedContents map[int]PastedContent) []contracts.ContentBlock {
	ids := sortedPastedIDs(pastedContents)
	blocks := make([]contracts.ContentBlock, 0, len(ids))
	for _, id := range ids {
		content := pastedContents[id]
		if content.Type != PastedContentImage || content.Content == "" {
			continue
		}
		blocks = append(blocks, contracts.NewBase64ImageBlock(content.MediaType, content.Content))
	}
	return blocks
}

func promptImageMetadataBlocks(pastedContents map[int]PastedContent) []contracts.ContentBlock {
	ids := sortedPastedIDs(pastedContents)
	blocks := make([]contracts.ContentBlock, 0, len(ids))
	for _, id := range ids {
		content := pastedContents[id]
		if content.Type != PastedContentImage {
			continue
		}
		sourcePath := imageSourcePath(id, content)
		text := ImageMetadataText(content.Dimensions, sourcePath)
		if text == "" {
			continue
		}
		blocks = append(blocks, contracts.NewTextBlock(text))
	}
	return blocks
}

func ImageMetadataText(dimensions *ImageDimensions, sourcePath string) string {
	if dimensions == nil ||
		dimensions.OriginalWidth <= 0 ||
		dimensions.OriginalHeight <= 0 ||
		dimensions.DisplayWidth <= 0 ||
		dimensions.DisplayHeight <= 0 {
		if sourcePath != "" {
			return "[Image source: " + sourcePath + "]"
		}
		return ""
	}
	wasResized := dimensions.OriginalWidth != dimensions.DisplayWidth || dimensions.OriginalHeight != dimensions.DisplayHeight
	if !wasResized && sourcePath == "" {
		return ""
	}
	parts := []string{}
	if sourcePath != "" {
		parts = append(parts, "source: "+sourcePath)
	}
	if wasResized {
		scaleFactor := float64(dimensions.OriginalWidth) / float64(dimensions.DisplayWidth)
		scaleFactor = math.Round(scaleFactor*100) / 100
		parts = append(parts, fmt.Sprintf(
			"original %dx%d, displayed at %dx%d. Multiply coordinates by %.2f to map to original image.",
			dimensions.OriginalWidth,
			dimensions.OriginalHeight,
			dimensions.DisplayWidth,
			dimensions.DisplayHeight,
			scaleFactor,
		))
	}
	return "[Image: " + strings.Join(parts, ", ") + "]"
}

func imageSourcePath(id int, content PastedContent) string {
	if content.SourcePath != "" {
		return content.SourcePath
	}
	path, ok := GetStoredImagePath(id)
	if !ok {
		return ""
	}
	return path
}

func sortedPastedIDs(pastedContents map[int]PastedContent) []int {
	ids := make([]int, 0, len(pastedContents))
	for id := range pastedContents {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}
