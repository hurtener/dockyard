package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/tool"
)

// TestContentBuilderRoutesStandardBlocks proves the additive builder path
// preserves UI metadata, text-first ordering, standard content, structured
// output, and result metadata without changing Builder/Result.
func TestContentBuilderRoutesStandardBlocks(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	uri := registerRevenueApp(t, s)
	if err := tool.NewContent[revenueInput, revenueOutput]("show_media").
		Describe("render generated media").
		UI("revenue_card").
		Handler(func(_ context.Context, in revenueInput) (tool.ContentResult[revenueOutput], error) {
			return tool.ContentResult[revenueOutput]{
				Text: "media ready",
				Content: []mcpsdk.Content{
					&mcpsdk.ImageContent{Data: []byte{1, 2}, MIMEType: "image/png"},
					&mcpsdk.ResourceLink{URI: "media://clip/0", Name: "clip.png", MIMEType: "image/png"},
				},
				Structured: revenueOutput{Headline: in.Period, Total: 1},
				Meta:       map[string]any{"source": "generated"},
			}, nil
		}).
		Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	list, err := connect(t, s).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want one", len(list.Tools))
	}
	ui := toolUIMeta(t, list.Tools[0].Meta)
	if ui["resourceUri"] != uri {
		t.Fatalf("content tool resourceUri = %v, want %q", ui["resourceUri"], uri)
	}

	res, err := connect(t, s).CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "show_media", Arguments: revenueInput{Period: "2026-Q1"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError || len(res.Content) != 3 {
		t.Fatalf("CallTool = error=%v content=%#v, want text plus two blocks", res.IsError, res.Content)
	}
	if got, ok := res.Content[0].(*mcpsdk.TextContent); !ok || got.Text != "media ready" {
		t.Fatalf("content[0] = %#v, want text-first block", res.Content[0])
	}
	if _, ok := res.Content[1].(*mcpsdk.ImageContent); !ok {
		t.Fatalf("content[1] = %T, want ImageContent", res.Content[1])
	}
	if _, ok := res.Content[2].(*mcpsdk.ResourceLink); !ok {
		t.Fatalf("content[2] = %T, want ResourceLink", res.Content[2])
	}
	if res.Meta["source"] != "generated" {
		t.Fatalf("result _meta = %#v, want source=generated", res.Meta)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var out revenueOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if out.Headline != "2026-Q1" || out.Total != 1 {
		t.Fatalf("structuredContent = %+v, want preserved output", out)
	}
}

// TestContentBuilderRejectsMissingHandler proves the new builder has the same
// loud registration behavior as Builder while exposing no continuation method.
func TestContentBuilderRejectsMissingHandler(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	if err := tool.NewContent[revenueInput, revenueOutput]("no_handler").Register(s); err == nil {
		t.Fatal("content builder without a handler should error")
	}
	if err := tool.NewContent[revenueInput, revenueOutput]("").
		Handler(func(context.Context, revenueInput) (tool.ContentResult[revenueOutput], error) {
			return tool.ContentResult[revenueOutput]{}, nil
		}).Register(s); err == nil {
		t.Fatal("content builder with an empty name should error")
	}
}
