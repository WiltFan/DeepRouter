# Official DeepRouter links (image-related)

Checked 2026-11-16. Pages may change; the live docs win over this list.

| Topic | Link |
| --- | --- |
| Docs home | https://deeprouter.apifox.cn |
| Docs index (llms.txt) | https://deeprouter.apifox.cn/llms.txt |
| Quick start (token, base URL, common errors) | https://deeprouter.apifox.cn/7453799m0.md |
| API hosts and quota-check page | https://deeprouter.apifox.cn/7453804m0.md |
| Token groups / pricing concepts | https://deeprouter.apifox.cn/7453801m0.md |
| Create gpt-image-2 (`POST /v1/images/generations`) | https://deeprouter.apifox.cn/489001962e0.md |
| Edit gpt-image-2 (`POST /v1/images/edits`) | https://deeprouter.apifox.cn/489001961e0.md |
| Create gpt-image-1.5 | https://deeprouter.apifox.cn/489001958e0.md |
| Create gpt-image-1 | https://deeprouter.apifox.cn/489001955e0.md |
| Gemini native image generation | https://deeprouter.apifox.cn/489001914e0.md |
| Google Gemini image guide | https://ai.google.dev/gemini-api/docs/image-generation |
| OpenAI Images reference (linked from the official pages) | https://platform.openai.com/docs/api-reference/images |

Console: https://deeprouter.top (令牌, 模型广场, 钱包).

## Notes on the official pages

- Server is `https://deeprouter.top`; paths already start with `/v1`. With an SDK base of `https://deeprouter.top/v1`, append only `/images/...`.
- The gpt-image-2 / gpt-image-1.5 create pages show a chat-completion example response. It is not the image response; read `data[].b64_json` / `data[].url` (or a single `data` object, as in the edit schema).
- Generate documents `format` (png/jpeg/webp). It does not document `output_format`, GIF, SVG, frame fields, or an `svg` object.
- Edit `background`: text says `opaque`/`auto`; the example value is `transparent`. Inconsistent.
- Gemini native: schema lists a required `key` query parameter and bearer security; the working combination is not verified.
- No official pages exist for gpt-image-2.5 / gpt-image-2.6, or for a programmatic balance / key-validation endpoint.
