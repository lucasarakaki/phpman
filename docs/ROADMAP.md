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
   journal antes e rollback automático best-effort quando falhar no meio.
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

- A execução real ficou para o M1.2; nesta fatia o `--dry-run` era obrigatório.
- Sury/ondrej é apenas sinalizado, sem passos para adicionar repositório.
- A TUI ainda não oferece ações de escrita.

### M1.2 — executor e helper privilegiado (concluído)

- [x] Executor sequencial: roda cada `argv`, verifica o passo e para no primeiro erro.
- [x] Confirmação humana em `install`, `remove` e `switch`; `--yes`/`-y` para automação.
- [x] `--dry-run` continua sem sudo e `--json` exige `--dry-run`.
- [x] Helper `__helper` por JSON Lines em socket Unix, iniciado por
  `sudo -- <phpman> __helper --socket <path>`.
- [x] Validação do plano em memória, da ordem dos passos, dos IDs e do `argv` exato.
- [x] Allowlist: `apt-get install/purge` de pacotes PHP versionados, `systemctl` de
  unidades `php*-fpm.service` e `update-alternatives --set` das entradas do PHP.
- [x] Resolução de binários por caminho absoluto confiável, sem `PATH` hijack.
- [x] Journal `/var/lib/phpman/journal.jsonl` escrito apenas pelo helper, com início e fim
  de cada comando, código de saída e erro.
- [x] Verificadores `packages-installed`, `packages-absent`, `binary-version`,
  `unit-active`, `unit-inactive`, `socket-exists` e `alternative-current`.
- [x] TUI e subcomandos de escrita recusam execução como root.
- [x] Testes de allowlist, protocolo, journal, executor e verificadores.
- [x] Smoke real em `debian:12` e `ubuntu:24.04`: sudo, socket, allowlist,
  `apt-get install` e journal funcionaram; a execução parou no `systemctl` porque os
  containers não têm systemd, como esperado.

Limitações desta fatia:

- O rollback é exibido e validado, mas ainda não é executado automaticamente (concluído no
  M1.3).
- `switch` só altera `update-alternatives`; não reinicia FPM nem reescreve vhosts
  (restart de FPM concluído no M1.3; vhosts ficam para o M3).
- A TUI continua somente leitura (M1.4).
- Sury/ondrej continua apenas sinalizado; a inclusão opt-in entra no M1.3b.
- O helper é aberto por comando. Reutilizar uma sessão no TUI será decidido no M1.4.
- O teste de execução real em container com systemd entra no M1.5.

### M1.3 — rollback, health check e switch seguro (em andamento)

- [x] Protocolo do helper aceita uma fase `rollback` separada, com ordem e `argv` validados
  contra o plano em memória.
- [x] Executor executa o rollback automaticamente, best-effort, na mesma sessão do helper,
  e retorna o erro original junto com eventuais falhas de rollback.
- [x] Interrupção por sinal não dispara rollback; o journal e o plano de rollback continuam
  disponíveis para intervenção manual.
- [x] Health check de instalação: pacotes, FPM ativo, FPM habilitado no boot, binário e
  socket (opcional).
- [x] `switch` reinicia o FPM da versão alvo, valida a unidade e o socket e para o FPM no
  rollback quando ele foi iniciado pelo plano.
- [x] Testes de rollback, ordem de fases, `unit-enabled` e restart de FPM no switch.
- [ ] Sury/ondrej opt-in com confirmação explícita, URL, fingerprint e keyring
  (`signed-by`), em fatia separada (M1.3b).
- [ ] Rollback de configuração de repositório e arquivos removidos por `purge` continua
  best-effort e não transacional.
- [ ] Vhosts não são reescritos; isso fica no M3.

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
- `--dry-run` imprime o plano e não pede sudo; sem ele, há confirmação e `--yes` pula a
  pergunta.
- Falha no meio grava o journal, para a execução e dispara o rollback automático
  best-effort dos passos planejados.

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

No M1.2 o CLI abre **um helper por plano**; a reutilização por sessão de TUI será tratada
no M1.4:

```
CLI/UI (usuário) --sudo--> <phpman> __helper --socket <path>
       │                                      │
       └──────── JSON Lines no socket Unix ───┘
```

O sudo lê a senha diretamente do terminal. O phpman não usa `-S`, não lê a senha e não a
coloca em argv, ambiente, log ou journal. O socket fica em um diretório 0700 do usuário,
com arquivo 0600; `SO_PEERCRED` confirma root no cliente e o `SUDO_UID` no helper.

Mensagens do cliente:

```json
{"op":"plan","plan":{"schema":1,"action":"install","version":"8.3","steps":[...],"rollback":[...]}}
{"op":"exec","id":"install-packages","argv":["apt-get","install","-y","php8.3-fpm"]}
{"op":"rollback","id":"rollback-remove-packages","argv":["apt-get","purge","-y","php8.3-fpm"]}
{"op":"done"}
```

Eventos do helper:

```json
{"kind":"ready"}
{"kind":"stdout","id":"install-packages","line":"..."}
{"kind":"stderr","id":"install-packages","line":"..."}
{"kind":"exit","id":"install-packages","exit":0}
{"kind":"error","id":"install-packages","error":"..."}
```

Regras de segurança implementadas:

- Senha lida pelo sudo, nunca logada, nunca em argv, nunca em arquivo.
- Helper recusa `sh`, `bash`, redirecionamentos e binários fora da allowlist.
- Cada `exec` ou `rollback` referencia um passo do plano recebido em memória; o helper
  valida schema, ação, versão, ID, ordem, fase, `argv` exato e `needs_root`.
- Binários são resolvidos por caminhos absolutos confiáveis, nunca por `PATH`.
- O JSON Lines não passa pelo stdin/stdout do sudo: usa um socket Unix privado, o que
  evita echo/tradução de bytes quando o sudo habilita `use_pty`.
- Allowlist: `apt-get install/purge -y` de pacotes PHP versionados; `systemctl`
  enable/disable/start/stop/restart de `php*-fpm.service`; `update-alternatives --set`
  de `php`, `phpize`, `php-config`, `phpdbg` e `phar`.
- Journal em `/var/lib/phpman/journal.jsonl`, escrito apenas pelo helper, com `start` e
  `finish`, código de saída e erro.
- O executável do helper precisa ser root-owned, não gravável por grupo/outros e estar em
  diretório root-owned; `./bin/phpman` e `go run` são recusados para execução.
- O helper encerra no `done` ou no EOF do cliente; não há sudo persistente.

### Testes

- Unitários: geração de plano, parsing de saídas, allowlist, protocolo, journal, executor e
  verificadores.
- Containers (podman/docker) em CI: Debian 12/13, Ubuntu 22.04/24.04.
- E2E real (com systemd) em VM ou podman com systemd: instalar → trocar → remover.
- Teste de contrato: `--dry-run` deve produzir exatamente os `argv` executados.

### Critérios de aceite

- `--dry-run` e leitura continuam funcionando sem sudo.
- O helper recusa `argv` divergente, passo fora de ordem, comando fora da allowlist e
  plano sem root.
- Nenhuma senha em log, argv, journal ou arquivo temporário.
- Falha simulada no meio do plano deixa o journal consistente; a execução para no primeiro
  erro e dispara o rollback automático best-effort.
- Container limpo → instalar 8.2 → instalar 8.3 via repo externo → trocar → remover 8.2 →
  estado original, em Debian 12 e Ubuntu 24.04 (E2E no M1.5).

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
