# Intraday AI Predict (Codex)

Clona a ideia do `gen-ai-hello-notes`: executa o Codex em loop dentro de uma pasta de dados e grava as previsoes em um arquivo.

## Como usar (host)

1) Coloque os dados em `./.data/intraday-ai-predict/workspace` (essa pasta sera o workspace do Codex).

2) Execute no host:

```sh
scripts/run-intraday-host.sh
```

## Variaveis (prefixo `INTRADAY_AI_PREDICT_`)

- `INTRADAY_AI_PREDICT_PROMPT_FILE`: caminho do arquivo de prompt (default: `/data/intraday-ai-predict/AI_README.md`, montado de `services/intraday-ai-predict/AI_README.md`)
- `INTRADAY_AI_PREDICT_PROMPT`: prompt principal (default: `Execute os comandos: mkdir hello-world; ls. Depois finalize respondendo com END_SESSION.`)
- `INTRADAY_AI_PREDICT_STOP_TOKEN`: token que, quando aparecer na resposta, encerra o loop (default: `END_SESSION`)
- `INTRADAY_AI_PREDICT_MODEL`: modelo do Codex (default: vazio para usar o modelo padrao do codex)
- `INTRADAY_AI_PREDICT_MODE`: modo do Codex (`suggest`, `auto-edit`, `full-auto`; default: `full-auto`)
- `INTRADAY_AI_PREDICT_CODEX_ARGS`: args extras para o `codex exec` (default: `--dangerously-bypass-approvals-and-sandbox`)
- `INTRADAY_AI_PREDICT_CLEAR_ON_START`: limpa `DATA_ROOT` no start (`1`/`true` para ativar)
- `INTRADAY_AI_PREDICT_DATA_ROOT`: raiz de dados a limpar (default: `/data/intraday-ai-predict`)
- `INTRADAY_AI_PREDICT_INTERVAL_SECONDS`: intervalo entre execucoes (default: 3600)
- `INTRADAY_AI_PREDICT_WORKSPACE`: caminho do workspace (default: `.data/intraday-ai-predict/workspace`)
- `INTRADAY_AI_PREDICT_OUT_FILE`: arquivo de saida (default: `.data/intraday-ai-predict/predictions.md`)

## Saida

As previsoes sao adicionadas em `./.data/intraday-ai-predict/predictions.md`.
