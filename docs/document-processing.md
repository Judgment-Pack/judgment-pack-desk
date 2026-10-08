# Document processing and OCR

A PDF's embedded text is extracted on this computer by the bundled gateway. A
page with no text (a scan) is marked **Needs OCR** unless an OCR processor is
chosen. The processor is chosen under **Scanned pages** in Document processing,
on a desk whose local gateway is in use; a desk that uses another gateway takes
that gateway's OCR, which its operator sets. OCR is chosen separately from the
chat model and the AI connections.

## What the settings are

The settings are the local gateway's (gateway v0.10.0): its connections
companion keeps them, with any cloud credential, in its private store under this
desk's `gateway-connections` directory (files `0600` in `0700` folders), never
in the project or in Desk's own configuration. Each setting:

- **OCR mode**: Off (the default), or **When a page has no text**.
- **OCR processor**: one of up to 16 processors on this computer.
- **Processing timeout**: 10 to 120 seconds per document, text extraction and
  OCR together; 120 when not set.

The processors:

| Processor | Where a page goes | What it needs |
| --- | --- | --- |
| Local OCR (Tesseract, English) | nowhere: read on this computer | `pdftoppm` (Poppler) and `tesseract` with English data in `/usr/bin` |
| OCR program on this computer | the program, given the PDF and the page numbers | an absolute path in the OCR tools bundle beside the gateway, or in `/usr/bin` |
| Google Document AI | Google, one image per page that needs OCR | project, location, Document OCR processor ID, service account JSON; `pdftoppm` |
| Azure Document Intelligence (prebuilt-read) | the resource's endpoint under `cognitiveservices.azure.com` | the endpoint and its key; `pdftoppm` |
| Amazon Textract (DetectDocumentText) | `textract.<region>.amazonaws.com` | the region, an access key ID and secret access key (and a session token, if any); `pdftoppm` |

Desk's bundle carries the two OCR workers the gateway runs, `ocr-tesseract` and
`ocr-cloud`; Poppler and Tesseract are not bundled. A processor whose programs
are not there says **Not available on this computer**, and a read under it stops
with `processor-not-installed` rather than reading without OCR. Only pages that
need OCR are sent to a cloud processor, and the provider may charge for each.
No processor falls back to another.

The settings apply to new PDFs from uploads, Drive, connected files (S3
included) and public links. Text already extracted stays as it is.

## A credential, entered once

A cloud processor's credential is typed into Document processing and saved
once. It travels in one request, the save's body, from the page to Desk and
from Desk to this desk's own connections companion, and to nothing else:

- Desk keeps none of it: it is not logged, not written to Desk's configuration or
  storage, and not put in a URL (a request carrying a query is refused unread).
- Desk sends it only to the local gateway's companion; while the desk uses
  another gateway, the request is refused and nothing is sent.
- No answer carries it. The companion says only that a credential is held, and
  Desk rebuilds each answer from the members it names, none of which is a
  credential; an answer that holds a credential the save carried is refused
  whole, and the page says the settings may have been saved. Desk compares
  exact bytes: the whole credential and, of an AWS key pair or a Google
  service account, each secret member, as typed and as an answer escapes them.
  An echo that is split, re-cased or interrupted is not caught by Desk; the
  gateway's own check, which normalises the text, is the one that holds those.
- A blank credential keeps the saved one only while the processor's destination
  (its endpoint, project, location, processor, region or program) is unchanged.
  Change the destination and the credential is entered again.

A save that is not the latest is refused (`processing-changed`): reload the
settings, then save again.

## The local gateway restarts when OCR turns on or off

The gateway reads the settings when Desk asks it for its plan, and Desk asks
once, when it starts the local gateway. With OCR off its plan is the one every
earlier Desk takes; with a processor chosen, the plan gives the document sources
(`documents`, `drive`, `web`, `aws-s3`) 150 seconds and launches them with
`--document-processing`, so that they read the settings. So:

- **A save that turns OCR on or off restarts the local gateway**, which stops the
  reads it is carrying. The page says so before the save, and the button reads
  **Save and restart**. Any other save (a processor's name, the timeout) restarts
  nothing.
- Document processing says which plan the running gateway has: whether it reads
  scanned pages with OCR. Where that is not what the settings say (they were
  changed elsewhere), it says the gateway takes them when it starts again.

Desk takes a source envelope longer than 60 seconds only as the gateway's plan
gives it: up to 150 seconds for a document source launched with
`--document-processing`, given to all four or none, and up to 130 seconds for
`web-search` launched with `--long-search` ([web search](web-search.md)). A plan
with anything else is refused whole, and the local gateway does not start. While
the running plan gives the document sources 150 seconds, Desk's research relay
gives a document read 160 seconds overall and 155 between bytes; otherwise its
ordinary bounds.

## Test a PDF

**Test a PDF** sends one PDF of at most 4 MiB through a saved processor and shows
the record's status, its error codes and up to eight pages of up to 400
characters each. Nothing is kept: the text is not saved to a chat or sent to the
assistant. A PDF with a text layer does not run OCR, so it does not test a
processor's credential; use a scanned PDF.

## What a record says

A page read by OCR is marked so in the document's record, which names the
processor's kind and program file and the file's digest (for example
`azure:ocr-cloud`), never a directory. That is the gateway's account of which
program read the page, under which settings; it does not establish that the
original had no text layer, nor anything about a cloud provider's handling of
the page once sent.

## Not covered

- A real call to any cloud provider has not been made from Desk: there are no
  credentials on the build machine.
- On macOS, Poppler and Tesseract are not in `/usr/bin`, so local OCR and the
  cloud processors say **Not available on this computer** unless the OCR tools
  bundle is installed beside the gateway; a program in `/usr/bin` can still be
  chosen.
- Removing a processor is not offered: disable it instead.
