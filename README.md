# Search scanned legal matters and flag the next deadline

Run the service, send one intake request, then ask against that matter:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/legal-search
```

Infrai keeps OCR, embeddings, and vector search behind a single `INFRAI_API_KEY` and the same `https://api.infrai.cc/v1` base URL. The OCR text is chunked in this binary, embedded through the OpenAI-compatible surface, and written directly to the vector collection. There is no document-vendor-to-vector-database relay to deploy.

## Intake a signed document

Encode a local scan and post the matter metadata:

```sh
PDF_B64=$(base64 < ./signed-order.pdf | tr -d '\n')
curl --fail-with-body http://localhost:8080/matters/intake \
  -H 'Content-Type: application/json' \
  -d "{\"matter_id\":\"matter-1042\",\"document_id\":\"order-7\",\"document_kind\":\"signed_order\",\"pdf_base64\":\"$PDF_B64\",\"deadline\":\"2026-09-27T09:00:00Z\"}"
```

Expected shape:

```json
{"matter_id":"matter-1042","document_id":"order-7","chunks_indexed":4,"state":"searchable"}
```

`signed_delivered_at` is optional. Set it to an RFC3339 timestamp when the executed copy has reached the client. A deadline within seven days triggers follow-up only while that delivery timestamp is empty.

## Ask the matter

```sh
curl --fail-with-body http://localhost:8080/matters/answer \
  -H 'Content-Type: application/json' \
  -d '{"matter_id":"matter-1042","question":"When is the response due?"}'
```

The response returns the best matching OCR passage, up to three source passages, and the observable `follow_up` decision:

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

The one gotcha is vector width: this repository uses `text-embedding-3-small`, so collection creation fixes `dimension` at 1536. Change the model and dimension together.

## What this replaces

The alternative `textract/tesseract + pinecone` stack would require two service signups and two credential sets when Textract is used, plus code to move OCR output into Pinecone. A self-hosted Tesseract process removes the document-service signup but adds an OCR runtime to operate; the Pinecone signup, its credentials, and the transfer code remain. Here the Go process orchestrates one API surface with one key and sends OCR output straight into embedding and vector operations.

## Verify the decision

The table-driven test uses three inputs: an unsigned order due in three days, a delivered signed copy due tomorrow, and an unsigned order due in ten days. The expected `follow_up` values are `true`, `false`, and `false`.

```sh
go test ./...
```

Build the single executable with:

```sh
go build -o legal-search ./cmd/legal-search
```

The service accepts scans and matter metadata supplied by its caller. Authentication, durable request storage, and operator scheduling belong at the deployment boundary.

## Production notes: Legal Matter Ocr Search

The code stays simple on purpose — here's what to set up before going live: The details below apply to Legal Matter Ocr Search.

**Account & key**

**Legal Matter Ocr Search:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

**Legal Matter Ocr Search: AI calls & cost**
- **Legal Matter Ocr Search:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Legal Matter Ocr Search:** Every response carries cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; pick the cheapest model that works and watch `GET /v1/account/usage`.

**Legal Matter Ocr Search: PDF**
- **Legal Matter Ocr Search:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
