# Roadmap e decisões de arquitetura

Este documento registra o escopo de cada marco, os critérios de aceite e as decisões que
não devem ser revertidas sem discussão.

## Princípios

1. **O núcleo não conhece a UI.** Detecção, plano e execução vivem em pacotes próprios;
   TUI e CLI são consumidores.
2. **Nada acontece sem plano.** Toda ação de escrita produz uma lista de passos exibível
   antes da execução (`--dry-run` mostra exatamente os `argv`).
3. **Privilégio mínimo.** A interface roda como o usuário; só um helper efêmero roda como
   root, por ação, com comandos em allowlist.
4. **Nunca `sh -c`.** Comandos são `argv`; nada de interpolação de string.
5. **Toda ação é verificável e reversível na medida do possível.** Health check depois,
   journal antes, rollback best-effort quando falhar no meio.
6. **Determinismo nos testes.** Parsers são testados com fixtures; integração roda em
   containers, não na máquina do desenvolvedor.

## M0 — inventário e diagnóstico (concluído)

Entregue:

- [x] Detecção de distribuição via `/etc/os-release`, com família e gerenciador.
- [x] Descoberta de binários PHP e versão autoritativa por `php -v`.
- [x] SAPIs: CLI, FPM, CGI, phpdbg, Apache.
- [x] PHP do sistema vs. PHP do `PATH`.
- [x] Serviços `php-fpm` e sockets (configurados e vivos).
- [x] Vhosts nginx/Apache que consomem PHP, com arquivo:linha.
- [x] Pacotes instalados (dpkg/rpm) e extensões carregadas.
- [x] Catálogo apt com origem (distro/terceiros).
- [x] `status`, `status --json`, `doctor`, TUI com três abas.
- [x] Testes unitários de todos os parsers e do renderizador.

Critérios de aceite atendidos:

- Roda em Debian/Ubuntu/Fedora sem root e sem efeitos colaterais.
- Mesmo `Snapshot` alimenta texto, JSON e TUI.
- Nenhum comando montado por concatenação de string.
- Timeout em toda execução externa.
- Saída JSON com `schema` versionado.

Limitações conhecidas:

- Catálogo de pacotes apenas em apt.
- Sem escrita, sem extensões gerenciáveis, sem pin por projeto.
- Detecção de vhost é heurística (não expande `include` do nginx).

## M1 — instalar, remover e trocar (Debian/Ubuntu)

### M1.1 — planejamento e dry-run (concluído)

- [x] Modelo serializável `Plan`/`Step`, com verificadores declarativos.
- [x] Provider apt para `install`, `remove` e `switch`, sem efeitos colaterais.
- [x] `install/remove/switch --dry-run` e `--dry-run --json`.
- [x] Bloqueio da remoção da versão ativa.
- [x] Avisos de vhosts afetados e de repositório de terceiros.
- [x] Testes de `argv` exato, determinismo, bloqueios e ausência de shell.
- [x] Smoke test manual em `debian:12`.

Limitações desta fatia:

- A execução real ainda não existe; `--dry-run` é obrigatório.
- Sury/ondrej é apenas sinalizado, sem passos para adicionar repositório.
- A TUI ainda não oferece ações de escrita.

### Provider

```go
type Provider interface {
    PlanInstall(snap *core.Snapshot, raw string, opts Options) (*core.Plan, error)
    PlanRemove(snap *core.Snapshot, raw string) (*core.Plan, error)
    PlanSwitch(snap *core.Snapshot, raw string) (*core.Plan, error)
}
```

O provider consome o `Snapshot` já produzido pela detecção; não duplica inventário nem
executa comandos. `internal/provider/apt` é a primeira implementação. A matriz de
capacidades por família (YAML embutido com `go:embed`) substitui os `if` de distribuição
quando o segundo provider entrar.

### Plano e passos

```go
type Plan struct {
    Schema   int
    Action   Action
    Version  string
    Summary  string
    NeedsRoot bool
    Warnings []string
    Steps    []Step
    Rollback []Step
}

type Step struct {
    ID, Description string
    Argv            []string // vazio = verificação interna
    NeedsRoot       bool
    Verify          VerifyKind // verificador declarativo
    VerifyArgs      []string
    Optional        bool
}
```

Regras:

- `Plan` é serializável, sem funções nem timestamps, e idempotente na geração.
- `VerifyKind` é um enum de dados (`binary-version`, `unit-active`, `alternative-current`
  etc.); o executor traduz cada valor para a verificação concreta.
- O executor só aceita `argv`; verifica o passo anterior antes do próximo.
- `--dry-run` imprime o plano e não pede sudo.
- Falha no meio grava o journal e oferece rollback.

### Fluxo de instalação

1. Pré-flight: lock do apt, espaço em disco, `dpkg --audit`, pacotes quebrados.
2. Se a versão não estiver no repositório padrão, oferecer sury/ondrej **com confirmação
   explícita**, mostrando URL, fingerprint e arquivo de keyring (`signed-by`).
3. `apt-get update` somente se o repositório mudou.
4. `apt-get install -y --no-install-recommends php8.3-{cli,common,fpm,opcache}` + extras.
5. Health check: `php8.3 -v`, unidade FPM `enabled`, socket presente.
6. Só então oferecer tornar a versão ativa.

### Troca de versão

1. Detectar vhosts/pools que dependem do socket atual.
2. Avisar sobre o impacto antes de executar.
3. `update-alternatives --set php /usr/bin/php8.3` (e `phpize`, `php-config`, `phpdbg`, `phar`).
4. Reiniciar o FPM afetado e validar.
5. Se um vhost apontar para o socket antigo, oferecer atualização transacional com backup
   (M3) — em M1 apenas avisar.

### Remoção

- Bloquear remoção da versão ativa sem escolha de fallback.
- Bloquear remoção de versão referenciada por vhost sem confirmação reforçada.
- `apt-get purge 'php8.2-*'`, limpar `/etc/php/8.2`, remover entradas de alternatives.
- Remover repositório de terceiros apenas se nenhuma versão restante depender dele.

### Helper privilegiado

A UI inicia **um** helper por sessão:

```
UI (usuário) --sudo -S--> phpman __helper <--JSON lines--> exec/argv
```

Mensagens do cliente:

```json
{"id":1,"op":"exec","argv":["apt-get","install","-y","php8.3-fpm"],"step":"install:8.3:3"}
```

Eventos do helper:

```json
{"id":1,"stream":"stdout","line":"..."}
{"id":1,"exit":0}
```

Regras de segurança:

- Senha lida uma vez, nunca logada, nunca em argv, nunca em arquivo.
- Helper recusa `sh`, `bash`, redirecionamentos e binários fora da allowlist.
- Cada `exec` referencia um passo de um plano assinado em memória; o helper valida ID e
  `argv`.
- Journal em `/var/lib/phpman/journal.jsonl`, escrito apenas pelo helper.
- Sessão encerrada ao sair do TUI; sem sudo persistente.

### Testes

- Unitários: geração de plano, parsing de saídas, allowlist, rollback.
- Containers (podman/docker) em CI: Debian 12/13, Ubuntu 22.04/24.04.
- E2E real (com systemd) em VM ou podman com systemd: instalar → trocar → remover.
- Teste de contrato: `--dry-run` deve produzir exatamente os `argv` executados.

### Critérios de aceite

- Container limpo → instalar 8.2 → instalar 8.3 via repo externo → trocar → remover 8.2 →
  estado original, em Debian 12 e Ubuntu 24.04.
- Nenhuma senha em log, argv, journal ou arquivo temporário.
- Falha simulada no meio do plano deixa journal consistente e rollback executável.
- Leitura continua funcionando sem sudo.

## M2 — extensões e Fedora/RHEL

- Catálogo por versão: instalada / habilitada / carregada.
- `phpenmod -v X.Y <mod>` e `phpdismod`, restart do SAPI afetado, validação com `php -m`.
- PECL fora do escopo até haver toolchain e tratamento de falha de compilação.
- Provider dnf/Remi: pacotes `phpXY-php-*`, detecção de repositório, limites por release
  (distros recém-lançadas podem não ter Remi disponível — degradar com mensagem clara).

## M3 — Alpine/openSUSE, integração com servidores web

- apk (`php83`, `php83-fpm`, OpenRC) e zypper (`devel:languages:php`).
- Atualização transacional de `fastcgi_pass`/`SetHandler`/`ProxyPassMatch` com backup,
  diff e rollback.
- Detecção de dependências por site e por pool.
- Nginx: expansão de `include` para não gerar falso negativo.

## M4 — distribuição e i18n

- `goreleaser` + `nfpm`: `.deb`, `.rpm`, `.apk` e binário estático.
- Strings isoladas para i18n (pt-BR padrão, en).
- CI completo: matriz de containers, `-race`, lint, release assinada.
- Documentação de cada provider e da matriz de capacidades.

## Fora de escopo (por enquanto)

- Pin de versão por projeto com shims (modelo mise/asdf).
- Compilação de PHP a partir do código-fonte.
- Windows/macOS.
- Gerenciamento de Composer ou de outros runtimes.
- Edição arbitrária de `php.ini` pela TUI (mostrar caminhos e diff é suficiente por ora).
