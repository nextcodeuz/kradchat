//go:build js && wasm

package main

import (
	"syscall/js"

	"kradchat/internal/emoji"
)

// Emoji panel — qidiruv + kategoriya bo'yicha grid.
// Ma'lumot: internal/emoji paketidan wasm'ga kompilyatsiyalanadi.

type EmojiItem struct {
	Emoji string
	Name  string
}

func emojiSearch(q string, limit int) []EmojiItem {
	var src []emoji.Emoji
	if q == "" {
		src = emoji.List
	} else {
		src = emoji.Search(q, limit)
	}
	if len(src) > limit {
		src = src[:limit]
	}
	out := make([]EmojiItem, 0, len(src))
	for _, e := range src {
		out = append(out, EmojiItem{Emoji: e.Emoji, Name: e.Name})
	}
	return out
}

var emojiOpen bool
var emojiCategory = ""

func toggleEmoji() {
	panel := ByID("emoji-panel")
	if panel.IsNull() || panel.IsUndefined() {
		return
	}
	if emojiOpen {
		AddClass(panel, "hidden")
		emojiOpen = false
		return
	}
	RemoveClass(panel, "hidden")
	emojiOpen = true
	if ByID("emoji-grid").Get("children").Length() == 0 {
		renderEmojiGrid("")
	}
	ByID("emoji-search").Call("focus")
}

func renderEmojiGrid(q string) {
	grid := ByID("emoji-grid")
	if grid.IsNull() || grid.IsUndefined() {
		return
	}
	HTML(grid, "")
	emojis := emojiSearch(q, 120)
	for _, e := range emojis {
		cell := El("button", "emoji-cell")
		SetText(cell, e.Emoji)
		cell.Set("title", e.Name)
		SetData(cell, "emoji", e.Emoji)
		On(cell, "click", func(_ js.Value, _ []js.Value) {
			insertEmoji(GetData(cell, "emoji"))
		})
		Append(grid, cell)
	}
}

func insertEmoji(e string) {
	input := ByID("msg-input")
	if input.IsNull() || input.IsUndefined() {
		return
	}
	cur := input.Get("textContent").String()
	input.Set("textContent", cur+e)
	input.Call("focus")
	RemoveClass(input, "empty")
}
