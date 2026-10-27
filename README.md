# prismatic-rainbow

CLI para **Bitbucket Cloud** (API REST 2.0) para consultar y operar pull
requests desde la terminal. Está pensada para personas y, sobre todo, para
dos tipos de agente:

- **Agentes de código.** Leen el código de un PR sin clonar ni hacer checkout.
  Pueden pedir un diff filtrado y acotado, el archivo completo en la rama del
  PR, los hilos de comentarios con archivo/línea y las tareas.
- **Agente de QA (PR ↔ ClickUp).** Obtiene en una sola llamada el estado
  completo del PR (`pr context`) y las tareas de ClickUp que referencia
  (`pr links`), para contrastar los cambios con los criterios de aceptación.

Solo soporta Bitbucket Cloud (bitbucket.org). La CLI no llama a la API de
ClickUp. Detecta los IDs de tarea en el PR y la tarea se lee con la
herramienta de ClickUp del agente (por ejemplo el MCP oficial).

## Características

| Área | Qué ofrece |
|---|---|
| Salida para agentes | `-o json` en todos los comandos, con un modelo normalizado y estable. Los errores también salen en JSON, con `code`, `required_scopes` y `retryable`. |
| Diff | `pr diff` con `--path`/`--exclude` (globs, `**`), `--name-only`, `--context`, `--ignore-whitespace`, `--max-lines-per-file` (conserva 60% del inicio y 40% del final) y `--max-bytes`, que lista lo que quedó afuera en `omitted_files`. |
| Código del PR | `pr file <pr> <path>` lee el archivo completo en el commit de origen o de destino (`--lines 10:40`, soporta forks). `src cat`/`src ls --ref <rama/tag/sha>` leen cualquier ref vía API, sin clonar. |
| Estado del PR | Revisores con su decisión (`approved`/`changes_requested`/`pending`), `pr checks` (builds; exit 8 si hay pendientes) y `pr mergeability` (qué bloquea el merge). |
| Discusión | `pr comments` agrupa en hilos (resuelto o no, archivo:línea, respuestas). También `pr tasks`, `pr comment` (inline, `--reply-to`, `--from-line`, `--body-file -`), `pr approve` y `pr request-changes`. |
| ClickUp | `pr links` detecta `CU-<id>`, URLs de tareas, IDs custom, `#id` y el sufijo `[estado]`. `pr list --task <ID>` busca los PRs de una tarea. |
| Una sola llamada | `pr context [--diff]` devuelve el PR, checks, mergeability, archivos, commits, hilos, tareas y referencias de ClickUp. Si una sección falla, se reporta en `warnings` y el resto se devuelve igual. |
| PR implícito | `<pr>` acepta `42`, `#42`, la URL del PR, o nada (usa el PR abierto de la rama actual). |
| Auth | API token de usuario (email + token) o access token de workspace/repo (Bearer). `--with-token` lee el token de stdin. Usa las mismas variables que Bitbucket Pipelines. |
| Robustez | Reintenta con backoff ante 429/5xx (solo GET, respeta `Retry-After`) y sigue el redirect 302 del diff. |
| Escape hatch | `api <path>` con `{workspace}`/`{repo}` y `--paginate`. |

## Instalación

```sh
go install github.com/LeanMendez/prismatic-rainbow/cmd/prismatic-rainbow@latest
# o desde el clon (instala además el atajo `prb`):
make install
```

`make install` deja `prb` como symlink a `prismatic-rainbow` en `$(go env GOPATH)/bin`
(o `GOBIN`), así que `prb pr context 42` equivale a `prismatic-rainbow pr context 42`,
también para agentes que usan shells no interactivas. Esa carpeta tiene que estar en
el `PATH`. No se usa `pr` porque choca con `/usr/bin/pr` de coreutils.

## Autenticación

Hay dos opciones:

1. **API token de usuario.** Se crea en
   <https://id.atlassian.com/manage-profile/security/api-tokens> con
   "Create API token with scopes" y la app Bitbucket. Se usa con tu email por
   HTTP Basic. Atlassian eliminó las app passwords; esta es su reemplazo.
2. **Access token de workspace, proyecto o repo.** Se envía como Bearer, sin
   email. Es lo recomendado para agentes y bots: tiene rate limits más altos y
   no usa una identidad personal. Como no tiene usuario, `--mine` no funciona.

Scopes mínimos para lectura: `read:repository:bitbucket` y
`read:pullrequest:bitbucket`. `read:user:bitbucket` y
`read:workspace:bitbucket` son para `auth`, `--mine` y la selección de
workspace. `write:pullrequest:bitbucket` es para comentar, aprobar, crear y
mergear. Si falta un scope, el error lo nombra (`required_scopes` con
`-o json`).

```sh
prismatic-rainbow auth login                                   # interactivo
prismatic-rainbow auth login --email vos@empresa.com --token ATATT...
echo "$TOKEN" | prismatic-rainbow auth login --with-token -R empresa/proyecto
prismatic-rainbow auth status -o json
```

Las credenciales se guardan en `~/.config/prismatic-rainbow/config.yaml`
(`0600`).

Precedencia: flags > variables de entorno > config.

| Variable | Uso |
|---|---|
| `BITBUCKET_TOKEN` | API token o access token. Si se define, el email del config **no** se hereda. |
| `BITBUCKET_EMAIL` | Email para API tokens de usuario. Si está vacío, el token se envía como Bearer. |
| `BITBUCKET_WORKSPACE` / `BITBUCKET_REPO_SLUG` | Workspace y repo cuando no hay remote git. Son las mismas variables que exporta Bitbucket Pipelines. |
| `CLICKUP_CUSTOM_ID_PREFIXES` | Prefijos de IDs custom de ClickUp, p. ej. `ENG,OPS` (también `clickup_custom_id_prefixes` en el config). |
| `BITBUCKET_API_URL` | Base de la API (default `https://api.bitbucket.org/2.0`). |

El repo se resuelve en este orden: `--repo ws/repo` (o `-R`) > remote git
(`origin` primero; acepta `bitbucket.org` y `ssh.bitbucket.org`) >
`BITBUCKET_REPO_SLUG`.

## Comandos

```text
auth login | status
repo list | view
branch list [--match CU-86b1x2]
src cat <path> [--ref r] [--lines a:b]      src ls [path] [--ref r] [--depth n]
api <path> [-X M] [--input f] [--paginate]

pr list [--state open,merged|all] [--task ID] [--source b] [--dest b] [--search t] [--mine] [--query BBQL]
pr view [<pr>] [--web]
pr context [<pr>] [--diff …]          # bundle para agentes
pr diff [<pr>] [--name-only] [--path p]… [--exclude g]… [--context n] [--ignore-whitespace] [--max-lines-per-file n] [--max-bytes n]
pr files [<pr>]                        # diffstat
pr file [<pr>] <path> [--side source|destination] [--lines a:b]
pr commits [<pr>]
pr comments [<pr>] [--unresolved] [--inline]
pr tasks [<pr>] [--state resolved|unresolved]
pr checks [<pr>]                       # exit 0 ok · 1 falló · 8 pendiente
pr mergeability [<pr>]
pr links [<pr>] [--custom-prefix ENG]…
pr create --title … [--body-file f] [--dest b] [--reviewer a,b] [--draft]
pr comment [<pr>] --body … | --body-file - [--path p --line n | --from-line n] [--reply-to id] [--task]
pr approve | request-changes [<pr>] [--remove]
pr merge [<pr>] [--strategy merge_commit|squash|fast_forward] [--close-source-branch]
```

Códigos de salida: `0` ok · `1` error · `2` uso · `4` credenciales o
permisos · `8` checks pendientes (`pr checks`). Con `-o json` los errores se
escriben en stderr como `{"error":{"code","message","http_status","required_scopes","retryable"}}`.

### Referencias a ClickUp

`pr links` y `pr context` usan las convenciones de la integración ClickUp ↔
Bitbucket:

| Forma | Ejemplo | Confianza |
|---|---|---|
| `CU-<id>` (también como formato de rama `CU-<id>_<nombre>`) | `CU-86b1x2y3z` | alta |
| URL de tarea | `https://app.clickup.com/t/86b1x2y3z`, `…/t/<team>/ENG-42` | alta |
| ID custom con prefijo configurado | `ENG-123` | alta |
| `#<id>` (6 a 12 caracteres, con letras y dígitos) | `#86b1x2y3z` | baja |

Busca en el título, la rama, la descripción, los mensajes de commit y los
comentarios (ClickUp publica un comentario con el link cuando vincula un PR).
Un sufijo `[estado]` (por ejemplo `CU-abc[ready]`) se informa en
`status_hint`.

## Skill para agentes

[`skills/prismatic-rainbow/SKILL.md`](skills/prismatic-rainbow/SKILL.md)
describe los flujos de review de código y de verificación de criterios de
aceptación. Para instalarla, copiala o enlazala en `~/.claude/skills/` o en el
directorio de skills de tu agente.

## Desarrollo

```sh
make build   # bin/prismatic-rainbow
make test    # go test ./...
make lint    # go vet + gofmt
```

Las fixtures de `internal/api/testdata` y `internal/diffparse/testdata` son
respuestas reales de la API para el PR público `atlassian/amps#1255`. El test
de `diffparse` compara el diff parseado con el diffstat que devuelve Bitbucket.
