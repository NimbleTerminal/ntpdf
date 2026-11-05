# Security policy and boundaries

## Reporting a vulnerability

Report vulnerabilities privately to **security@neomantra.net**. Include the
ntpdf version or commit, operating system, affected loading/rendering path, and
a minimal reproduction or sample PDF. Do not put malicious documents or exploit
details in public issues.

## Choosing a loading path

PDF input, document text, metadata, filenames, and worker output are untrusted.
The security boundary depends on the renderer factory selected by the host:

| Path | Where PDF parsing and rendering happen | Boundary |
| --- | --- | --- |
| Default native widget | Text extraction and PDF preflight in the host; PDFium runs through wazero in the host process | Resource checks and WASM isolation, without an OS-confined child process |
| `WorkerRendererFactory` | PDF preflight, text extraction, page metadata, and rasterization inside an ntmediabox worker | OS process confinement plus hostile-peer protocol and metadata validation |
| Browser WASM | Text extraction in the browser's Go WASM runtime; PDFium through `ntshim/pdfium` | Browser/WASM boundaries; no ntmediabox OS subprocess |
| Direct `pdfdoc.Open` | In the calling process | Resource checks and panic recovery; no process confinement |
| Custom factory or `WorkerConfig.Start` | Chosen by the application | Application responsibility |

The normal demos use the default rendering path. Selecting ImageMode alone does
not enable OS confinement. Applications handling hostile PDFs should explicitly
select `WorkerRendererFactory` and leave `AllowUnsandboxed` false.

For confined widget loads, the host reads or copies input bytes and sends
them to the worker. It consumes validated page boxes, positioned text runs, and
image counts from the worker, without calling the PDF parser on that input.
Worker startup, parsing, timeout, protocol, or metadata failures fail the load.
There is no automatic host PDF parsing or TextMode fallback after a confined
load failure. Local renderer failures may still fall back to local text mode.
A host that explicitly retries with a local factory is choosing a different
security boundary.

## Configuring the worker

Use the same limits for host file reads, worker requests, and worker serving:

```go
limits := ntpdf.Limits{MaxFileBytes: 32 << 20, MaxRenderPixels: 4 << 20}
pv := ntpdf.NewWithConfig(ntpdf.Config{
    Cols: 80, Rows: 24,
    Limits: limits,
    RendererFactory: ntpdf.WorkerRendererFactory(ntpdf.WorkerConfig{
        WorkerPath: "/absolute/path/to/pdf-worker",
        Limits: limits,
        Timeout: 10 * time.Second,
    }),
})
defer pv.Close()
```

The dedicated worker executable must enter worker serving before doing any
interactive application initialization:

```go
func main() {
    conn := ntmediabox.WorkerConn()
    defer conn.Close()
    limits := ntpdf.Limits{MaxFileBytes: 32 << 20, MaxRenderPixels: 4 << 20}
    if err := ntpdf.ServeWorker(conn, limits); err != nil {
        os.Exit(1)
    }
}
```

A combined application/worker binary can instead dispatch a dedicated flag to
this serving code before starting its TUI. Set `WorkerArgs` to that flag; an empty
`WorkerPath` uses the current executable. Do not launch the normal TUI as a
worker or write logs to the protocol stream.

On Linux, deploy a verified `ntconfine-stage` beside the host executable or supply
`StagePath` explicitly. A stage integrated into the worker binary requires an
early stage dispatch and explicit `StageArgs`; merely selecting the worker
executable is insufficient. Check `ntmediabox.Probe()` for platform support, but
also handle startup failures: a probe does not guarantee a particular policy can
be applied.

The default launcher verifies worker/stage executable ownership and permissions
through ntmediabox/ntconfine. It grants the worker the default library/font read
paths and execution of the verified worker and required dynamic loaders. It does
not grant document-directory access: document bytes travel over the protocol.
The application controls the source file path and must authorize that host-side
read itself.

On supported Linux systems, ntconfine applies Landlock and seccomp. On supported
macOS systems it uses `sandbox-exec`. Unsupported or unavailable confinement
fails closed by default. `AllowUnsandboxed: true` explicitly permits a child
process without OS confinement when enforcement is unavailable. `Start` replaces
the launcher entirely; it is a trusted integration hook and must provide its own
confinement, an interruptible connection, and cleanup that kills and reaps the
worker. See the upstream
[ntmediabox security policy](https://github.com/NimbleTerminal/ntmediabox/blob/main/SECURITY.md)
for its protocol and OS confinement boundaries.

## Limits, deadlines, and hostile worker output

- `Config.Limits.MaxFileBytes` bounds both in-memory input and the actual file
  read, including a file that grows after its size is checked. Zero uses the
  documented default; a negative value removes the application cap. These
  checks do not authorize paths or impose a timeout on host filesystem I/O.
  For in-memory loads, applications must bound buffers before supplying them:
  defensive ownership copies precede asynchronous load validation.
- `WorkerConfig.Limits` is sent with `pdf-open`. The worker combines positive
  requested limits with its own serving limits by taking the stricter value.
  A request cannot relax the worker's configured caps. The session retains the
  resulting parsing, DPI, and pixel limits. Use matching host and worker limits;
  host stream limits do not automatically configure a separately supplied factory.
- Worker open (including preflight and text extraction) and render RPCs have a
  default timeout of **15 seconds**. `WorkerConfig.Timeout` changes that bound;
  non-positive values use the default. `RenderPageContext` also honors a shorter
  caller deadline. Cancellation and transport/protocol errors close the
  connection and invoke cleanup. `Close` can interrupt an in-flight RPC without
  waiting for the rendering state lock. The timeout covers protocol operations,
  not a custom `Start` function or host file reads.
- ntmediabox validates frame lengths, dimensions, pixel budgets, and exact RGBA
  byte counts before allocating a host image. Its hard limits include **128 MiB
  input**, **8192 pixels per side**, and **32 Mi pixels per returned image**.
  Negative ntpdf limits cannot remove these bounds. The worker also checks
  session, request, and wire image budgets before allocating an RGBA conversion
  buffer; an application image-budget error can leave the session usable.
- PDF worker metadata uses a required versioned envelope. The host rejects
  missing/unknown versions, inconsistent page counts, invalid geometry, and
  invalid text/image counts. It caps metadata at **8 MiB**, **10,000 pages**,
  **100,000 total text runs**, and **1024 image placeholders per page**. Stricter
  configured page/image caps apply as well. Array counts and nesting/token
  budgets are checked before allocating typed page/run collections. Coordinates
  must be finite and within ±1 billion points; font names and individual text
  strings are bounded at 4096 bytes and 64 KiB respectively. Old workers without
  this envelope are rejected; deploy host and worker builds together.
- ntmediabox sanitizes worker error strings. Metadata bytes are structured
  untrusted data and are never printed directly. ntpdf filters non-printable
  characters when placing text and sanitizes its own status labels. Applications
  displaying filenames, extracted strings, or errors themselves must call
  `ntpdf.SanitizeForTerminal`.

## Limits of the guarantee

The local `pdfdoc` preflight checks stream/filter inflation budgets, cross-reference
and object-stream boundaries, page counts, and bounded tree/form traversal.
Downstream parser calls have panic recovery. These checks cannot preempt every
CPU loop or memory allocation in an in-process parser. A recovered panic also
does not establish that a document is safe.

Confinement restricts worker authority; it does not make the PDF decoder or its
reported text trustworthy. Wire limits bound returned objects, rather than all
worker heap usage or the aggregate memory of many workers. Apply external memory,
CPU, and concurrency controls where those guarantees are required. The OS/kernel,
ntconfine, ntmediabox, verified executable installation, host configuration, and
structured metadata decoder remain trusted components. Do not store secrets in
the library/font directories intentionally readable by the worker.

The widget's confined loading path does not confine separate application calls to
`pdfdoc.Open`, custom renderers, or example code that parses documents directly.
Wrappers around a worker factory must preserve its returned renderer capabilities
and errors; the package's own limits wrapper does so. Arbitrary custom wrappers
are trusted application code and can change the boundary.

## Review and validation

Preserve these invariants when changing worker integration:

- Confined loading never reparses PDF bytes in the host or falls back after a
  worker failure.
- Metadata is bounded and validated before it drives host allocation or layout;
  extracted strings cannot inject terminal controls.
- Requests have deadlines; cancellation, disposal, and invalid peer responses
  release the connection and worker exactly once.
- Worker limits cannot be relaxed by open/render requests, and image dimensions
  are checked before conversion or host allocation.
- Keep tests for malformed metadata, startup failure, stalled open/render RPCs,
  concurrent disposal, session caps, and sandboxed subprocess rendering.

Run native tests with the race detector, integration tests, vet, native and WASM
builds/tests, and the browser demo smoke checks. Sandboxed subprocess tests may
skip when the host cannot enforce confinement; such a skip is not validation of
that platform's sandbox.
