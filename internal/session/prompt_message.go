package session

import (
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
		path, ok := GetStoredImagePath(id)
		if !ok || path == "" {
			continue
		}
		blocks = append(blocks, contracts.NewTextBlock("[Image source: "+path+"]"))
	}
	return blocks
}

func sortedPastedIDs(pastedContents map[int]PastedContent) []int {
	ids := make([]int, 0, len(pastedContents))
	for id := range pastedContents {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}
