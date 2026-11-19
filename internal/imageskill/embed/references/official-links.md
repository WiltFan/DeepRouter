# Official DeepRouter links (image-related)

Checked November 16, 2026. Grok, Doubao, and DALL·E rows were checked November 17. Gemini native pages were checked November 19.

| Topic | Link |
| --- | --- |
| Docs home | https://deeprouter.apifox.cn |
| Docs index (llms.txt) | https://deeprouter.apifox.cn/llms.txt |
| Quick start (token, base URL, common errors) | https://deeprouter.apifox.cn/7453799m0.md |
| API debugging (Bearer header) | https://deeprouter.apifox.cn/7453797m0.md |
| API hosts and quota-check page | https://deeprouter.apifox.cn/7453804m0.md |
| Token groups / pricing concepts | https://deeprouter.apifox.cn/7453801m0.md |
| Create gpt-image-2 (`POST /v1/images/generations`) | https://deeprouter.apifox.cn/489001962e0.md |
| Edit gpt-image-2 (`POST /v1/images/edits`) | https://deeprouter.apifox.cn/489001961e0.md |
| Create gpt-image-1.5 | https://deeprouter.apifox.cn/489001958e0.md |
| Create gpt-image-1 | https://deeprouter.apifox.cn/489001955e0.md |
| Grok 创建 Image (`POST /v1/images/generations`, e.g. `grok-3-image`) | https://deeprouter.apifox.cn/489001977e0.md |
| Grok 编辑 image (`POST /v1/images/edits`) | https://deeprouter.apifox.cn/489001978e0.md |
| Doubao 创建图片 (`POST /v1/images/generations`, `doubao-seedream-*` / `doubao-seededit-*`) | https://deeprouter.apifox.cn/489001979e0.md |
| Gemini native image generation and editing (`POST /v1beta/models/{model}:generateContent`) | https://deeprouter.apifox.cn/489001914e0.md |
| Cherry Studio tutorial (Nano Banana as `gemini-3-pro-image-preview`, Gemini protocol) | https://deeprouter.apifox.cn/7453863m0.md |
| Google Gemini image guide | https://ai.google.dev/gemini-api/docs/image-generation |
| Google Gemini API authentication | https://ai.google.dev/api |
| Google Gemini model catalog | https://ai.google.dev/api/models |
| OpenAI Images reference (linked from the official pages) | https://platform.openai.com/docs/api-reference/images |

Console: https://deeprouter.top (令牌, 模型广场, 钱包).

## Notes on the official pages

- Server is `https://deeprouter.top`; paths already start with `/v1`. With an SDK base of `https://deeprouter.top/v1`, append only `/images/...`.
- The gpt-image-2 / gpt-image-1.5 create pages show a chat-completion example response. It is not the image response; read `data[].b64_json` / `data[].url` (or a single `data` object, as in the edit schema).
- Generate documents `format` (png/jpeg/webp). It does not document `output_format`, GIF, SVG, frame fields, or an `svg` object.
- Edit `background`: text says `opaque`/`auto`; the example value is `transparent`. Inconsistent.
- The helper implements the DeepRouter Gemini request shape with `contents`, `inline_data` for edit inputs, and `generationConfig.responseModalities` set to `IMAGE`. DeepRouter's official debug instructions say to send `Authorization: Bearer`. Its `generateContent` OpenAPI declares bearer security and also lists a required query `key`. The helper uses Bearer for DeepRouter-compatible gateways and `x-goog-api-key` for `generativelanguage.googleapis.com`. Credentials stay out of URLs, logs, redirects, and result downloads. Bearer-only acceptance, native generation and editing, and the native URL response layout remain unverified by live POST.
- The Image Skill wizard uses `GET /v1/models` for DeepRouter-compatible gateways and `GET /v1beta/models` for the official Google host entered as a custom HTTPS address. It reads only the first page of Google's list.
- Grok and Doubao image pages use the same Images paths; Grok video pages are video, not image. Midjourney is a task API and FLUX also has a Replicate task format; neither is the Images API.
- 图像生成 > 创建 DALL·E 3 is also the Images API (example model `dall-e-3`; the text also names `dall-e-2`).
- `deeprouter`'s picker lists a model from `GET /v1/models` when its endpoint types name the Images API, or its id is documented above: the GPT-Image ids and any other `gpt-image-*` id, `flux-kontext-pro` / `flux-kontext-max`, `grok-3-image`, `dall-e-3` / `dall-e-2`, `doubao-seedream-*` / `doubao-seededit-*`. Listed means visible to the token, not that the account may use it; nothing is test-generated. Gemini / Nano Banana, Midjourney, and video ids are never on that list.
- No official pages exist for gpt-image-2.5 / gpt-image-2.6 (including `gpt-image-2.6-flare`), for Grok image ids other than `grok-3-image`, or for a programmatic balance / key-validation endpoint.
