# phpman

![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![Licença MIT](https://img.shields.io/badge/licen%C3%A7a-MIT-green)
![Status M1](https://img.shields.io/badge/status-M1%20conclu%C3%ADdo-blue)

Inventário e diagnóstico de instalações PHP em distribuições Linux, com uma interface de
terminal no estilo lazydocker.

O phpman responde, em um só lugar, perguntas que costumam exigir meia dúzia de comandos:
qual PHP está realmente ativo, qual socket o FPM usa, por que um site quebrou depois de
uma atualização e por que o `php -v` do terminal não é o mesmo PHP que o nginx executa.

> **Status: M1 concluído — v0.2.0.** O phpman detecta o ambiente, aponta problemas,
> gera planos e aplica `install`, `remove` e `switch` em Debian/Ubuntu por um helper
> privilegiado efêmero, com allowlist, verificação passo a passo, journal e rollback
> automático best-effort em caso de falha. A troca reinicia e valida o FPM da versão alvo.
> Na TUI, a aba *Versões* permite instalar, trocar e remover com prévia rolável do plano;
> a execução suspende a interface e usa o mesmo CLI, que refaz o plano e pede a
> confirmação final no terminal. Quando a versão não está no catálogo da distribuição, o
> plano mostra o repositório Sury/ondrej com URL, fingerprint e keyring `signed-by`
> explícitos antes da confirmação. A validação M1.5 cobre Debian 12/13 e Ubuntu
> 22.04/24.04 em containers com systemd, o contrato entre `--dry-run --json` e o journal,
> e a falha no meio do plano com rollback automático. O M1 foi publicado como `v0.2.0`.
> A detecção e o `--dry-run` continuam sem root. O plano completo está em
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
  `systemctl` de unidades PHP-FPM, `update-alternatives` do PHP e os comandos fixos de
  download e ajuste do keyring Sury/ondrej.
- Verifica o estado depois de cada passo, incluindo FPM ativo e habilitado no boot,
  conteúdo do source list e fingerprint da chave PGP.
- Executa o rollback do plano automaticamente, best-effort, quando um passo falha.
- Reinicia e valida o FPM da versão alvo ao trocar a versão ativa.
- Registra comandos, escritas de arquivo, códigos de saída e erros em
  `/var/lib/phpman/journal.jsonl`.
- Bloqueia a remoção da versão ativa e avisa sobre vhosts afetados.
- Oferece Sury/ondrej apenas com `--third-party`, mostrando URL, fingerprint e keyring
  `signed-by` antes da confirmação.

**Interface**

- TUI com abas *Visão geral*, *Versões* e *Doutor*, responsiva a terminais estreitos.
- Ações de instalar, trocar e remover na aba *Versões*, com prévia rolável do plano,
  avisos e rollback antes da execução.
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
| `phpman install 8.4 --third-party` | permite o repositório Sury/ondrej se a versão não estiver no catálogo |
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
phpman install 8.4 --third-party --dry-run
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

### Repositório Sury/ondrej (opt-in)

Quando a versão pedida não aparece no catálogo da distribuição, o phpman não adiciona
repositórios por conta própria. O plano falha com uma mensagem clara e a opção
`--third-party` precisa ser passada explicitamente. A flag só se aplica a `install`;
`remove` e `switch` nunca adicionam repositórios:

```bash
phpman install 8.4 --third-party --dry-run
```

O plano mostra, antes de qualquer alteração:

- URL do repositório: `https://packages.sury.org/php/`
- URL da chave: `https://packages.sury.org/php/apt.gpg`
- Keyring: `/usr/share/keyrings/deb.sury.org-php.gpg` (`signed-by`)
- Fingerprint esperado: `15058500A0235D97F5D10063B188E2B695BD4743`
- Source list: `/etc/apt/sources.list.d/sury-php.list`

Os passos adicionais são fixos e validados pela allowlist do helper:

1. instalar `ca-certificates`, `curl` e `gnupg` com `apt-get`;
2. baixar a chave por HTTPS com `curl` (sem `.curlrc`, com protocolo restrito a HTTPS);
3. ajustar o keyring para `0644` e conferir o fingerprint com `gpg`;
4. escrever o source list com `signed-by` e verificar o conteúdo;
5. rodar `apt-get update` e só então instalar os pacotes PHP.

O bootstrap usa os índices atuais do apt; em um sistema sem listas, rode
`sudo apt-get update` antes de iniciar o plano. O `apt-get update` do passo 5 é o que
atualiza os índices depois de adicionar o repositório Sury/ondrej.

O download da chave e a escrita do source list são operações privilegiadas separadas. O
helper aceita apenas o caminho, o modo e o formato exatos do repositório Sury/ondrej; não
existe uma primitiva genérica de escrita de arquivos. O rollback automático remove os
pacotes PHP instalados, mas **não** remove a configuração do repositório nem a chave; isso
é documentado no plano e pode ser revertido manualmente.

Sem `--dry-run`, o mesmo plano é exibido e o phpman pede confirmação antes de iniciar o
helper sudo:

```bash
phpman install 8.3        # pergunta "Executar este plano? [s/N]"
phpman remove 8.2 --yes   # automação: não pergunta
phpman switch 8.3 -y      # alias curto
```

O fluxo de execução:

1. O plano é validado e enviado ao helper por JSON Lines em um socket Unix privado.
2. O helper confere schema, ação, versão, ordem, IDs e `argv` exato (ou escrita de
   arquivo) de cada passo.
3. A allowlist aceita apenas `apt-get update`, `apt-get install/purge` de pacotes PHP
   versionados, o conjunto fixo de pacotes de bootstrap do Sury/ondrej, `systemctl`
   enable/disable/start/stop/restart de unidades `php*-fpm.service`,
   `update-alternatives --set` do PHP, o `curl` exato da chave Sury/ondrej e o `chmod`
   exato do keyring.
4. Cada comando roda sem shell, com ambiente não interativo e timeout.
5. O executor verifica o estado depois do passo; no primeiro erro obrigatório ele para.
6. Antes de retornar o erro, o executor executa o rollback do plano na mesma sessão do
   helper, em ordem, continuando mesmo se um passo de rollback falhar.
7. O helper grava início e fim de cada comando e de cada escrita de arquivo, inclusive
   rollback, em `/var/lib/phpman/journal.jsonl`.

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
- Repositórios de terceiros nunca são adicionados implicitamente. `--third-party` é
  obrigatório, o plano exibe URL, fingerprint e keyring `signed-by`, e a confirmação
  menciona o repositório Sury/ondrej.

## Interface

Exemplo ilustrativo (cores omitidas):

```
 ⬢ phpman 0.2.0-dev                                    Debian GNU/Linux 12 (bookworm)
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
| `i` | instalar uma versão (aba *Versões*) |
| `s` | trocar para a versão instalada selecionada |
| `d` | remover a versão instalada selecionada |
| `r` | redetectar o ambiente |
| `?` | abrir a ajuda |
| `q` / `ctrl+c` | sair |

Na aba *Versões*, `i` abre o campo de versão já preenchido com o item selecionado. Se a
versão não estiver no catálogo, o plano inclui os metadados do repositório Sury/ondrej e
só é executado depois da confirmação explícita. `s` e `d` planejam a troca e a remoção de
uma versão instalada. O plano completo aparece em um diálogo rolável; ao confirmar, a TUI
é suspensa e o mesmo binário executa o subcomando correspondente, refaz o plano e pede a
confirmação final no terminal. A TUI nunca roda como root e não lê a senha do sudo.

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
- Toda escrita exige plano, confirmação (ou `--yes`) e um helper sudo efêmero. As ações da
  TUI suspendem a interface e delegam ao mesmo CLI; não existe helper persistente nem
  execução privilegiada dentro da TUI.
- O helper valida o plano em memória, a ordem dos passos e o `argv` exato antes de executar.
- O protocolo usa um socket Unix 0600 em diretório 0700 do usuário; o cliente confere que
  o peer é root e o helper confere que o peer é o usuário que invocou o sudo.
- A allowlist restringe binários, pacotes, unidades systemd, alternatives do PHP e os
  comandos fixos de download e ajuste do keyring Sury/ondrej.
- A escrita do source list é uma operação específica, com caminho, modo e conteúdo
  validados de forma independente pelo helper; não existe escrita arbitrária de arquivos.
- O fingerprint da chave Sury/ondrej é conferido com `gpg` antes do `apt-get update`.
- Nenhum comando é montado por concatenação de string e nenhum shell (`sh`/`bash`) é aceito.
- O binário que o sudo executa precisa ser root-owned e não gravável por outros usuários.
- O sudo lê a senha diretamente do terminal; o phpman nunca vê, grava ou registra a senha.
- Timeout e cancelamento em toda execução externa.
- O journal é escrito somente pelo helper, em `/var/lib/phpman/journal.jsonl`, com hash
  SHA-256 das escritas de arquivo em vez do conteúdo bruto.
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

O teste de contrato compara o plano de `--dry-run --json` com os eventos `start` do
journal do helper, canonicalizando o caminho de `argv[0]`. A matriz E2E usa containers
privilegiados com systemd e valida o fluxo completo em Debian 12/13 e Ubuntu 22.04/24.04:
container limpo → instalar a versão base → instalar o target via Sury/ondrej → trocar a
versão ativa → remover a base. Um cenário separado roda em um container sem systemd PID 1
para forçar falha no `systemctl`, validar o journal e confirmar o rollback dos pacotes.

```bash
make test-e2e E2E_IMAGE=debian:12 E2E_BASE=8.2 E2E_TARGET=8.3
make test-rollback E2E_IMAGE=debian:12 E2E_VERSION=8.2
```

Os testes E2E exigem Docker, `jq` e um host que permita containers privilegiados. O CI
roda a matriz completa e o cenário de rollback em jobs separados.

## Roadmap

| Marco | Escopo |
|---|---|
| **M0** (concluído) | detecção, TUI somente leitura, `status --json`, `doctor` |
| **M1.1** (concluído) | planos apt de instalar/remover/trocar, `--dry-run` e JSON |
| **M1.2** (concluído) | executor, helper sudo, allowlist, journal e confirmação |
| **M1.3** (concluído) | rollback automático, health check e restart de FPM no switch |
| **M1.3b** (concluído) | Sury/ondrej opt-in com confirmação explícita, fingerprint e keyring |
| **M1.4** (concluído) | instalar, trocar e remover pela TUI com prévia do plano e confirmação final |
| **M1.5** (concluído) | matriz Debian/Ubuntu, E2E com systemd, contrato dry-run × journal, falha/rollback e release `v0.2.0` |
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
