package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/infrai-examples/legal-matter-search/internal/infrai"
)

const embeddingDimension = 1536

type Backend interface {
	OCR(context.Context, string, string, string) (string, error)
	Embed(context.Context, []string) ([][]float64, error)
	CreateCollection(context.Context, string, int, string) error
	Upsert(context.Context, string, []infrai.Vector, string) error
	Query(context.Context, string, []float64, string, int) ([]infrai.Match, error)
}

type Intake struct {
	MatterID          string    `json:"matter_id"`
	DocumentID        string    `json:"document_id"`
	DocumentKind      string    `json:"document_kind"`
	PDFBase64         string    `json:"pdf_base64"`
	OCRLanguage       string    `json:"ocr_language,omitempty"`
	SignedDeliveredAt time.Time `json:"signed_delivered_at,omitempty"`
	Deadline          time.Time `json:"deadline,omitempty"`
}

type IntakeResult struct {
	MatterID   string `json:"matter_id"`
	DocumentID string `json:"document_id"`
	Chunks     int    `json:"chunks_indexed"`
	State      string `json:"state"`
}

type Answer struct {
	MatterID      string   `json:"matter_id"`
	Question      string   `json:"question"`
	Answer        string   `json:"answer"`
	Sources       []string `json:"sources"`
	FollowUp      bool     `json:"follow_up"`
	FollowUpCause string   `json:"follow_up_cause,omitempty"`
}

type Service struct {
	backend    Backend
	collection string
	now        func() time.Time
}

func New(backend Backend, collection string) *Service {
	return &Service{backend: backend, collection: collection, now: time.Now}
}

func (s *Service) Ingest(ctx context.Context, in Intake) (IntakeResult, error) {
	if in.MatterID == "" || in.DocumentID == "" || in.DocumentKind == "" || in.PDFBase64 == "" {
		return IntakeResult{}, errors.New("matter_id, document_id, document_kind, and pdf_base64 are required")
	}
	requestID := stableID(in.MatterID, in.DocumentID)
	text, err := s.backend.OCR(ctx, in.PDFBase64, in.OCRLanguage, "ocr-"+requestID)
	if err != nil {
		return IntakeResult{}, err
	}
	chunks := splitText(text, 1200)
	if len(chunks) == 0 {
		return IntakeResult{}, errors.New("OCR produced no searchable text")
	}
	embeddings, err := s.backend.Embed(ctx, chunks)
	if err != nil {
		return IntakeResult{}, err
	}
	if len(embeddings) != len(chunks) {
		return IntakeResult{}, errors.New("embedding count does not match chunk count")
	}
	if err := s.backend.CreateCollection(ctx, s.collection, embeddingDimension, "collection-"+s.collection); err != nil {
		return IntakeResult{}, err
	}

	vectors := make([]infrai.Vector, len(chunks))
	for i := range chunks {
		vectors[i] = infrai.Vector{
			ID:     fmt.Sprintf("%s-%03d", requestID, i),
			Values: embeddings[i],
			Metadata: map[string]any{
				"matter_id":           in.MatterID,
				"document_id":         in.DocumentID,
				"document_kind":       in.DocumentKind,
				"text":                chunks[i],
				"signed_delivered_at": timeString(in.SignedDeliveredAt),
				"deadline":            timeString(in.Deadline),
			},
		}
	}
	if err := s.backend.Upsert(ctx, s.collection, vectors, "upsert-"+requestID); err != nil {
		return IntakeResult{}, err
	}
	return IntakeResult{MatterID: in.MatterID, DocumentID: in.DocumentID, Chunks: len(chunks), State: "searchable"}, nil
}

func (s *Service) Ask(ctx context.Context, matterID, question string) (Answer, error) {
	if matterID == "" || question == "" {
		return Answer{}, errors.New("matter_id and question are required")
	}
	embeddings, err := s.backend.Embed(ctx, []string{question})
	if err != nil {
		return Answer{}, err
	}
	if len(embeddings) != 1 {
		return Answer{}, errors.New("question embedding is missing")
	}
	matches, err := s.backend.Query(ctx, s.collection, embeddings[0], matterID, 3)
	if err != nil {
		return Answer{}, err
	}
	if len(matches) == 0 {
		return Answer{MatterID: matterID, Question: question, Answer: "No matching passage was found in this matter."}, nil
	}

	answer := Answer{MatterID: matterID, Question: question}
	for _, match := range matches {
		text, _ := match.Metadata["text"].(string)
		if text != "" {
			answer.Sources = append(answer.Sources, text)
		}
	}
	answer.Answer = answer.Sources[0]
	answer.FollowUp, answer.FollowUpCause = followUpDecision(s.now(), matches[0].Metadata)
	return answer, nil
}

func followUpDecision(now time.Time, metadata map[string]any) (bool, string) {
	deadlineRaw, _ := metadata["deadline"].(string)
	deliveredRaw, _ := metadata["signed_delivered_at"].(string)
	if deadlineRaw == "" || deliveredRaw != "" {
		return false, ""
	}
	deadline, err := time.Parse(time.RFC3339, deadlineRaw)
	if err != nil {
		return false, ""
	}
	remaining := deadline.Sub(now)
	if remaining >= 0 && remaining <= 7*24*time.Hour {
		return true, "deadline is within seven days and signed delivery is pending"
	}
	return false, ""
}

func splitText(text string, limit int) []string {
	words := strings.Fields(text)
	var chunks []string
	var current strings.Builder
	for _, word := range words {
		if current.Len() > 0 && current.Len()+1+len(word) > limit {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func timeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
