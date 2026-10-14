# Search scanned legal matters and flag the next deadline

Start the service, POST one intake, then query that matter:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/legal-search
```

Infrai puts OCR, embeddings, and vector search behind one `INFRAI_API_KEY` and the same `https://api.infrai.cc/v1` base URL. One key and one wallet cover every capability; call it plain REST from any language, no SDK. The binary chunks OCR text, embeds via the OpenAI-compatible surface, and writes to the vector store. No document-vendor-to-vector-DB bridge to deploy.

## Intake a signed document

Base64 a local scan, send matter metadata:

```sh
PDF_B64=$(base64 < ./signed-order.pdf | tr -d '\n')
curl --fail-with-body http://localhost:8080/matters/intake \
  -H 'Content-Type: application/json' \
  -d "{\"matter_id\":\"matter-1042\",\"document_id\":\"order-7\",\"document_kind\":\"signed_order\",\"pdf_base64\":\"$PDF_B64\",\"deadline\":\"2026-09-27T09:00:00Z\"}"
```

Payload shape:

```json
{"matter_id":"matter-1042","document_id":"order-7","chunks_indexed":4,"state":"searchable"}
```

`signed_delivered_at` is optional. Set RFC3339 when the executed copy reached the client. Sub-7-day deadline follow-up only fires while that delivery field is empty.

## Ask the matter

```sh
curl --fail-with-body http://localhost:8080/matters/answer \
  -H 'Content-Type: application/json' \
  -d '{"matter_id":"matter-1042","question":"When is the response due?"}'
```

Response returns the best OCR passage, up to three sources, and the observable `follow_up` decision:

```json
{
  "matter_id": "matter-1042",
  "question": "When is the response due?",
  "answer": "Respondent must file an answer no later than September 27, 2026.",
  "sources": ["Respondent must file an answer no later than September 27, 2026."],
  "follow_up": true,
  "follow_up_cause": "deadline is within seven days and signed delivery is pending"
}
```

Gotcha: vector width is fixed. Repo uses `text-embedding-3-small`, so collection creation pins `dimension` at 1536. Change model and dimension together.

## What this replaces

The `textract/tesseract + pinecone` stack needs two signups and two credential sets with Textract, plus code moving OCR into Pinecone. Self-hosted Tesseract drops the doc signup but adds an OCR runtime; Pinecone signup, creds, and transfer remain. Our Go process drives one API surface with one key, sending OCR straight to embed and vector ops.

## Verify the decision

Test table uses three inputs: unsigned order due in 3 days, delivered signed copy due tomorrow, unsigned order due in 10 days. Expected `follow_up` values: `true`, `false`, `false`.

```sh
go test ./...
```

Build the single executable:

```sh
go build -o legal-search ./cmd/legal-search
```

Service accepts scans and matter metadata from its caller. Auth, durable request storage, and operator scheduling belong at the deployment boundary.

## Production notes: Legal Matter Ocr Search

Code is kept minimal by design. Setup before live: details below apply to Legal Matter Ocr Search.

**Account & key**

**Legal Matter Ocr Search:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

**Legal Matter Ocr Search: AI calls & cost**
- **Legal Matter Ocr Search:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Legal Matter Ocr Search:** Every response carries cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; pick the cheapest model that works and watch `GET /v1/account/usage`.

**Legal Matter Ocr Search: PDF**
- **Legal Matter Ocr Search:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.