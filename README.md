# phpman

![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![Licença MIT](https://img.shields.io/badge/licen%C3%A7a-MIT-green)
![Status M1.3](https://img.shields.io/badge/status-M1.3%20rollback%20e%20health%20check-blue)

Inventário e diagnóstico de instalações PHP em distribuições Linux, com uma interface de
terminal no estilo lazydocker.

O phpman responde, em um só lugar, perguntas que costumam exigir meia dúzia de comandos:
qual PHP está realmente ativo, qual socket o FPM usa, por que um site quebrou depois de
uma atualização e por que o `php -v` do terminal não é o mesmo PHP que o nginx executa.

> **Status: M1.3 — rollback e health check.** O phpman detecta o ambiente, aponta
> problemas, gera planos e aplica `install`, `remove` e `switch` em Debian/Ubuntu por um
> helper privilegiado efêmero, com allowlist, verificação passo a passo, journal e
> rollback automático best-effort em caso de falha. A troca reinicia e valida o FPM da
> versão alvo. A detecção e o `--dry-run` continuam sem root. Sury/ondrej opt-in e as
> ações na TUI entram nas próximas fatias. O plano completo está em
> [docs/ROADMAP.md](docs/ROADMAP.md).

## Sumário

- [Por que phpman](#por-que-phpman)
- [Recursos](#recursos)
- [Instalação](#instalação)
- [Uso](#uso)
- [Planejamento seguro](#planejamento-seguro)
- [Interface](#interface)
- [Saída JSON](#saída-json)
- [Como a detecção funciona](#como-a-detecção-funciona)
- [Compatibilidade](#compatibilidade)
- [Segurança](#segurança)
- [Arquitetura](#arquitetura)
- [Desenvolvimento](#desenvolvimento)
- [Roadmap](#roadmap)
- [Contribuindo](#contribuindo)
- [Licença](#licença)

## Por que phpman

Gerenciar PHP no Linux não é difícil por causa do `apt install`. É difícil pelo que vem
depois: descobrir qual das três versões instaladas está ativa, se o FPM está parado, se o
site aponta para um socket que não existe mais, se a extensão está instalada mas
desabilitada, e como desfazer uma mudança sem deixar o servidor em um estado ambíguo.

O phpman trata o ambiente PHP como um inventário: primeiro **observa** tudo, depois
**diagnostica**, e só então **planeja e executa** mudanças com confirmação, privilégio
mínimo e rollback automático best-effort.

## Recursos

**Inventário**

- Descobre todas as instalações em `/usr/bin`, `/usr/sbin` e `/usr/local/{bin,sbin}`,
  incluindo builds compilados localmente.
- Obtém a versão do próprio binário (`php -v`), não do nome do arquivo.
- Separa as SAPIs: CLI, FPM, CGI, phpdbg e módulo do Apache (`mod_php`).
- Compara o PHP do sistema (`update-alternatives`) com o PHP do `PATH` do usuário.
- Lista pacotes PHP instalados via dpkg/rpm e extensões carregadas por versão.
- Lê `php.ini` e o diretório de configuração de cada SAPI.

**Diagnóstico**

- Consulta unidades `php-fpm` no systemd e o estado de cada uma.
- Encontra sockets configurados nos pools e sockets vivos em `/run`.
- Varre nginx e Apache e lista os sites que consomem PHP, com arquivo e linha.
- Em Debian/Ubuntu, consulta o catálogo do apt e identifica a origem da versão
  (distro ou repositório de terceiros, como sury/ondrej).
- Emite avisos acionáveis com códigos estáveis (`fpm-inactive`, `socket-missing`,
  `vhost-socket-missing`, `path-mismatch`, `no-php`...).

**Planejamento e execução**

- Gera planos de instalação, remoção e troca para Debian/Ubuntu.
- Mostra os `argv` exatos, os verificadores e o rollback que será executado se algo falhar.
- Exporta o plano em JSON (`schema: 1`) para revisão e testes de contrato.
- Executa com confirmação (`--yes` para automação) por um helper sudo efêmero.
- Valida cada passo contra o plano em memória e contra uma allowlist: `apt-get`,
  `systemctl` de unidades PHP-FPM e `update-alternatives` do PHP.
- Verifica o estado depois de cada passo, incluindo FPM ativo e habilitado no boot.
- Executa o rollback do plano automaticamente, best-effort, quando um passo falha.
- Reinicia e valida o FPM da versão alvo ao trocar a versão ativa.
- Registra comandos, códigos de saída e erros em `/var/lib/phpman/journal.jsonl`.
- Bloqueia a remoção da versão ativa e avisa sobre vhosts afetados.

**Interface**

- TUI com abas *Visão geral*, *Versões* e *Doutor*, responsiva a terminais estreitos.
- `status` em texto, `status --json` para scripts e `doctor` para diagnóstico rápido.
- Fallback automático para texto quando não há TTY.

## Instalação

Requisitos para compilar: **Go 1.24+** e Linux.

```bash
git clone https://github.com/lucasarakaki/phpman.git
cd phpman
make build
sudo make install
```

O `make install` copia o binário para `/usr/local/bin/phpman`. O caminho precisa ser
root-owned e não gravável por outros usuários: é esse executável que o sudo inicia como
helper. Rodar `./bin/phpman` ou um binário em `$HOME/go/bin` continua servindo para
`status`, `doctor` e `--dry-run`, mas a execução de planos é recusada por segurança.

Alternativa sem clonar o repositório (somente leitura/dry-run, a menos que o binário seja
instalado depois em um diretório root-owned):

```bash
go install github.com/lucasarakaki/phpman/cmd/phpman@latest
```

## Uso

| Comando | Descrição |
|---|---|
| `phpman` | abre a interface interativa |
| `phpman status` | relatório completo do ambiente |
| `phpman status --json` | inventário completo em JSON |
| `phpman doctor` | apenas diagnósticos e sugestões |
| `phpman install 8.3` | instala uma versão, com confirmação e sudo |
| `phpman remove 8.2` | remove uma versão, com confirmação e sudo |
| `phpman switch 8.3` | troca a versão ativa, com confirmação e sudo |
| `phpman install 8.3 --dry-run` | simula a instalação de uma versão |
| `phpman remove 8.2 --dry-run` | simula a remoção de uma versão |
| `phpman switch 8.3 --dry-run` | simula a troca da versão ativa |
| `phpman install 8.3 --yes` | executa sem pedir confirmação (automação) |
| `phpman version` | mostra a versão |
| `phpman help` | mostra a ajuda |

Sem TTY (por exemplo em um pipe), `phpman` sem argumentos imprime o relatório em texto.

## Planejamento e execução

Todo comando de escrita primeiro produz um plano revisável com os comandos exatos, os
verificadores e o rollback. `--dry-run` para nesse ponto, sem sudo:

```bash
phpman install 8.3 --dry-run
phpman remove 8.2 --dry-run
phpman switch 8.3 --dry-run
```

Exemplo de saída:

```
phpman plano · simulação, nada será alterado
Instalar PHP 8.3 (CLI, FPM e OPcache)
ação install · versão 8.3 · requer root: sim

PASSOS
  1. Instalar php8.3-cli, php8.3-common, php8.3-fpm, php8.3-opcache
     apt-get install -y --no-install-recommends php8.3-cli php8.3-common php8.3-fpm php8.3-opcache
  2. Habilitar e iniciar o FPM
     systemctl enable --now php8.3-fpm.service
  3. Confirmar que o FPM está habilitado no boot
     verificação: unit-enabled php8.3-fpm.service
  4. Confirmar que o binário responde com a versão esperada
     verificação: binary-version /usr/bin/php8.3 8.3
  5. Confirmar que o socket padrão do FPM foi criado
     verificação: socket-exists /run/php/php8.3-fpm.sock
     (opcional)

ROLLBACK
  1. Parar e desabilitar o FPM instalado
     systemctl disable --now php8.3-fpm.service
  2. Remover os pacotes instalados pelo plano
     apt-get purge -y php8.3-cli php8.3-common php8.3-fpm php8.3-opcache
```

Sem `--dry-run`, o mesmo plano é exibido e o phpman pede confirmação antes de iniciar o
helper sudo:

```bash
phpman install 8.3        # pergunta "Executar este plano? [s/N]"
phpman remove 8.2 --yes   # automação: não pergunta
phpman switch 8.3 -y      # alias curto
```

O fluxo de execução:

1. O plano é validado e enviado ao helper por JSON Lines em um socket Unix privado.
2. O helper confere schema, ação, versão, ordem, IDs e `argv` exato de cada passo.
3. A allowlist aceita apenas `apt-get install/purge` de pacotes PHP versionados,
   `systemctl enable/disable/start/stop/restart` de unidades `php*-fpm.service` e
   `update-alternatives --set` de `php`, `phpize`, `php-config`, `phpdbg` e `phar`.
4. Cada comando roda sem shell, com ambiente não interativo e timeout.
5. O executor verifica o estado depois do passo; no primeiro erro obrigatório ele para.
6. Antes de retornar o erro, o executor executa o rollback do plano na mesma sessão do
   helper, em ordem, continuando mesmo se um passo de rollback falhar.
7. O helper grava início e fim de cada comando, inclusive rollback, em
   `/var/lib/phpman/journal.jsonl`.

Para inspecionar o plano em scripts:

```bash
phpman install 8.3 --dry-run --json | jq '.steps[].argv'
```

As regras atuais:

- `--json` exige `--dry-run`; a saída JSON nunca executa.
- Sem TTY, a execução exige `--yes`; não há confirmação implícita em pipes.
- O plano nunca usa shell: cada passo é uma lista de argumentos.
- Remover a versão ativa é bloqueado; troque de versão primeiro.
- A remoção avisa quais vhosts usam o socket da versão.
- Se um passo obrigatório falhar, o rollback do plano é executado automaticamente e
  best-effort; interrupções por sinal não disparam rollback.
- `switch` altera `update-alternatives`, reinicia o FPM da versão alvo e valida a unidade
  e o socket. Vhosts que apontam para o socket de outra versão ainda não são reescritos.
- Repositórios de terceiros são apenas sinalizados nesta etapa; a inclusão de sury/ondrej
  com confirmação explícita entra numa fatia seguinte.

## Interface

Exemplo ilustrativo (cores omitidas):

```
 ⬢ phpman 0.1.0-dev                                    Debian GNU/Linux 12 (bookworm)
 PHP 8.2 ativo  ·  FPM 1/1 ativo  ·  2 versões  ·  1 aviso
  1 Visão geral  2 Versões  3 Doutor

╭──────────────────────────╮  ╭──────────────────────────╮  ╭──────────────────────────╮  ╭──────────────────────────╮
│ DISTRO                   │  │ PHP ATIVO                │  │ FPM                      │  │ DIAGNÓSTICO              │
│ Debian GNU/Linux 12      │  │ 8.2.29                   │  │ 1/1 ativo                │  │ 1 aviso                  │
│ DEBIAN · apt             │  │ também no PATH           │  │ unidades php-fpm         │  │ veja a aba Doutor        │
╰──────────────────────────╯  ╰──────────────────────────╯  ╰──────────────────────────╯  ╰──────────────────────────╯

VERSÕES
  ● 8.2   ativa
  + 8.3   disponível

AVISOS
  ! Um site aponta para o socket /run/php/php8.1-fpm.sock, que não existe.
  i 2 versões de PHP instaladas: 8.3, 8.2.
```

Teclas:

| Tecla | Ação |
|---|---|
| `tab` / `←` `→` | trocar de aba |
| `1` `2` `3` | ir direto para uma aba |
| `↑` `↓` / `k` `j` | navegar na lista |
| `g` / `G` | primeiro / último item |
| `r` | redetectar o ambiente |
| `?` | abrir a ajuda |
| `q` / `ctrl+c` | sair |

A TUI continua somente leitura no M1.3; as ações de escrita pela interface entram no M1.4.

## Saída JSON

```bash
phpman status --json | jq '.versions[] | {raw, active, sapis: [.sapis[].kind]}'
```

```json
{
  "schema": 1,
  "generated_at": "2026-10-21T14:28:58-03:00",
  "distro": {
    "id": "debian",
    "pretty_name": "Debian GNU/Linux 12 (bookworm)",
    "family": "debian",
    "package_manager": "apt"
  },
  "active_version": "8.2",
  "path_version": "8.2",
  "path_binary": "/usr/bin/php8.2",
  "versions": [
    {
      "raw": "8.2",
      "full": "8.2.29",
      "active": true,
      "in_path": true,
      "sapis": [
        { "kind": "cli", "binary": "/usr/bin/php8.2" },
        { "kind": "fpm", "unit": "php8.2-fpm.service", "active": true }
      ]
    }
  ],
  "warnings": [
    {
      "severity": "warn",
      "code": "fpm-inactive",
      "message": "O FPM do PHP 8.2 está parado.",
      "hint": "sudo systemctl start php8.2-fpm.service"
    }
  ]
}
```

O payload tem `schema: 1` e é estável para consumo por scripts. Mudanças incompatíveis
avançam o campo `schema`.

## Como a detecção funciona

| Dado | Fonte |
|---|---|
| Distribuição | `/etc/os-release` |
| Versões e SAPIs | binários `php*` executados com `-v` (a versão vem do próprio binário) |
| Versão ativa | `update-alternatives --query php` e `exec.LookPath("php")` |
| Serviços | arquivos unitários + `systemctl show` |
| Sockets | `listen =` nos pools FPM + sockets em `/run` |
| Sites | varredura de nginx/Apache por `fastcgi_pass`, `proxy_pass` e `SetHandler` |
| Pacotes | `dpkg-query` ou `rpm` |
| Extensões | `php -m` por versão |
| php.ini | `php --ini` |
| Catálogo (apt) | `apt-cache search` + `apt-cache policy` |

As consultas independentes rodam em paralelo e todas têm timeout (cerca de 1s em uma
máquina típica). Arquivos sem permissão de leitura são ignorados: o modo somente leitura
funciona sem root.

## Compatibilidade

| Família | Detecção | Catálogo de versões | Planejamento | Escrita |
|---|---|---|---|---|
| Debian/Ubuntu e derivados | completa | sim (apt) | sim (apt) | sim (apt) |
| Fedora | genérica | — | — | M2 |
| RHEL/Rocky/Alma | genérica | — | — | M2 |
| Arch/Manjaro | genérica | — | — | somente leitura (versão única) |
| Alpine | genérica | — | — | M3 |
| openSUSE/SLES | genérica | — | — | M3 |

"Genérica" significa que binários, SAPIs, serviços, sockets e vhosts são detectados; o
catálogo de pacotes e o planejamento dependem do provider da família.

## Segurança

- Detecção, `status`, `doctor` e `--dry-run` rodam sem root; a TUI recusa ser iniciada como
  root.
- Toda escrita exige plano, confirmação (ou `--yes`) e um helper sudo efêmero.
- O helper valida o plano em memória, a ordem dos passos e o `argv` exato antes de executar.
- O protocolo usa um socket Unix 0600 em diretório 0700 do usuário; o cliente confere que
  o peer é root e o helper confere que o peer é o usuário que invocou o sudo.
- A allowlist restringe binários, pacotes, unidades systemd e alternatives do PHP.
- Nenhum comando é montado por concatenação de string e nenhum shell (`sh`/`bash`) é aceito.
- O binário que o sudo executa precisa ser root-owned e não gravável por outros usuários.
- O sudo lê a senha diretamente do terminal; o phpman nunca vê, grava ou registra a senha.
- Timeout e cancelamento em toda execução externa.
- O journal é escrito somente pelo helper, em `/var/lib/phpman/journal.jsonl`.
- `NO_COLOR=1` desativa cores na saída de texto.

## Arquitetura

```
cmd/phpman/           entrada, subcomandos, flags e o entrypoint __helper
internal/core/        modelo de domínio (Snapshot, Version, Plan, Step...)
internal/detect/      os-release, binários PHP, systemd, sockets, vhosts, apt, dpkg/rpm
internal/executor/    executor, protocolo JSON Lines, allowlist, journal e verificadores
internal/provider/    contrato de planejamento e erros
internal/provider/apt planos apt de instalar, remover e trocar
internal/provider/registry  seleção do provider pela família
internal/report/      renderização em texto, doctor, planos e JSON
internal/tui/         interface bubbletea/lipgloss
docs/                 roadmap, decisões e critérios de aceite
```

Fluxo de dados:

```
os-release ──┐
binários ────┤
systemd ─────┼──> detect.Scan ──> core.Snapshot ──┬──> report (texto/JSON)
pools ───────┤                                    ├──> tui (bubbletea)
vhosts ──────┤                                    └──> provider/apt ──> core.Plan
apt/dpkg ────┘                                                              │
                                                                            ▼
                              executor ──sudo──> phpman __helper ──> allowlist
                                  │                    │
                                  └── verificadores    └── journal
```

O núcleo não conhece a UI. O mesmo `Snapshot` alimenta a TUI, o `status` e os planos. O
provider só lê o snapshot e devolve dados; o executor aplica o plano e o helper concentra o
privilégio e a allowlist.

## Desenvolvimento

```bash
make check     # gofmt + vet + testes
make test      # apenas testes
make build     # binário em bin/phpman
make run       # executa o TUI
```

Os parsers de os-release, `php -v`, `php --ini`, `php -m`, pools FPM, alternatives,
`apt-cache policy` e configurações de nginx/Apache têm testes unitários com fixtures.
Os planos do provider apt também são testados como dados: `argv` exato, bloqueios,
rollback, determinismo e ausência de shell/metacaracteres. O executor, a allowlist, o
protocolo JSON Lines, o journal e os verificadores têm testes unitários próprios; nenhum
teste unitário executa comandos reais como root.

O smoke test de leitura roda o binário em `debian:12` e gera um plano real de instalação
sem root. O smoke de execução em `debian:12` e `ubuntu:24.04` valida sudo, socket,
allowlist, `apt-get install` e journal até o `systemctl`; como esses containers não têm
systemd, o E2E completo com systemd e a matriz de containers entram no M1.5.

## Roadmap

| Marco | Escopo |
|---|---|
| **M0** (concluído) | detecção, TUI somente leitura, `status --json`, `doctor` |
| **M1.1** (concluído) | planos apt de instalar/remover/trocar, `--dry-run` e JSON |
| **M1.2** (concluído) | executor, helper sudo, allowlist, journal e confirmação |
| **M1.3** (atual) | rollback automático, health check e restart de FPM no switch |
| M1.3b | sury/ondrej opt-in com confirmação explícita |
| M1.4 | ações na TUI |
| M1.5 | containers Debian/Ubuntu e release |
| M2 | extensões por versão e suporte a Fedora/RHEL via Remi |
| M3 | Alpine/openSUSE, integração transacional com nginx/Apache |
| M4 | pacotes `.deb`/`.rpm`/`.apk`, i18n, CI completo |

Detalhes, critérios de aceite e o protocolo do helper privilegiado estão em
[docs/ROADMAP.md](docs/ROADMAP.md).

## Contribuindo

- Rode `make check` antes de abrir um pull request.
- Use commits convencionais (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `ci:`).
- Parsers novos precisam de teste com fixture; nada de depender da máquina local.
- Detecção e planejamento devem continuar funcionando sem root e sem efeitos colaterais.

### Fluxo de branches

- `main` contém apenas versões estáveis.
- `develop` é a branch de integração.
- Toda feature nasce de `develop` em uma branch própria:
  `feat/<escopo>`, `fix/<escopo>`, `docs/<escopo>` ou `chore/<escopo>`.
- O pull request tem `develop` como destino; não faça commit direto em `main` ou
  `develop`.
- Releases são cortadas a partir de `main`, com tag e release no GitHub.

## Licença

MIT — veja [LICENSE](LICENSE).
