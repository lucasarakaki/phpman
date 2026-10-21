# phpman

![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![Licença MIT](https://img.shields.io/badge/licen%C3%A7a-MIT-green)
![Status M0](https://img.shields.io/badge/status-M0%20somente%20leitura-blue)

Inventário e diagnóstico de instalações PHP em distribuições Linux, com uma interface de
terminal no estilo lazydocker.

O phpman responde, em um só lugar, perguntas que costumam exigir meia dúzia de comandos:
qual PHP está realmente ativo, qual socket o FPM usa, por que um site quebrou depois de
uma atualização e por que o `php -v` do terminal não é o mesmo PHP que o nginx executa.

> **Status: M0 — somente leitura.** O phpman detecta o ambiente e aponta problemas, mas
> ainda não instala, remove ou troca versões. Nada é alterado e nenhum acesso root é
> solicitado nesta versão. O plano completo está em [docs/ROADMAP.md](docs/ROADMAP.md).

## Sumário

- [Por que phpman](#por-que-phpman)
- [Recursos](#recursos)
- [Instalação](#instalação)
- [Uso](#uso)
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
**diagnostica**, e só então — nos próximos marcos — **planeja e executa** mudanças com
confirmação, privilégio mínimo e rollback.

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
sudo install -m 0755 bin/phpman /usr/local/bin/phpman
```

Alternativa sem clonar o repositório:

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
| `phpman version` | mostra a versão |
| `phpman help` | mostra a ajuda |

Sem TTY (por exemplo em um pipe), `phpman` sem argumentos imprime o relatório em texto.

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

| Família | Detecção | Catálogo de versões | Escrita |
|---|---|---|---|
| Debian/Ubuntu e derivados | completa | sim (apt) | planejada (M1) |
| Fedora | genérica | — | planejada (M2) |
| RHEL/Rocky/Alma | genérica | — | planejada (M2) |
| Arch/Manjaro | genérica | — | somente leitura (versão única) |
| Alpine | genérica | — | planejada (M3) |
| openSUSE/SLES | genérica | — | planejada (M3) |

"Genérica" significa que binários, SAPIs, serviços, sockets e vhosts são detectados; o
catálogo de pacotes depende do provider da família.

## Segurança

- M0 não executa nenhum comando de escrita e não pede sudo.
- Nenhum comando é montado por concatenação de string: apenas `argv`.
- Timeout e cancelamento em toda execução externa.
- Nenhuma senha é lida, gravada ou registrada.
- `NO_COLOR=1` desativa cores na saída de texto.

## Arquitetura

```
cmd/phpman/        entrada, subcomandos e flags
internal/core/     modelo de domínio (Version, SAPI, Snapshot, Warning...)
internal/detect/   os-release, binários PHP, systemd, sockets, vhosts, apt, dpkg/rpm
internal/report/   renderização em texto, doctor e JSON
internal/tui/      interface bubbletea/lipgloss
docs/              roadmap, decisões e critérios de aceite
```

Fluxo de dados:

```
os-release ──┐
binários ────┤
systemd ─────┼──> detect.Scan ──> core.Snapshot ──┬──> report (texto/JSON)
pools ───────┤                                    └──> tui (bubbletea)
vhosts ──────┤
apt/dpkg ────┘
```

O núcleo não conhece a UI. O mesmo `Snapshot` alimenta a TUI, o `status` e o JSON.

## Desenvolvimento

```bash
make check     # gofmt + vet + testes
make test      # apenas testes
make build     # binário em bin/phpman
make run       # executa o TUI
```

Os parsers de os-release, `php -v`, `php --ini`, `php -m`, pools FPM, alternatives,
`apt-cache policy` e configurações de nginx/Apache têm testes unitários com fixtures.
Testes de integração em containers (Debian, Ubuntu, Fedora, Alpine) entram no M1, junto
com as primeiras ações de escrita.

## Roadmap

| Marco | Escopo |
|---|---|
| **M0** (atual) | detecção, TUI somente leitura, `status --json`, `doctor` |
| M1 | instalar/remover/trocar no Debian/Ubuntu, plano + sudo + rollback |
| M2 | extensões por versão e suporte a Fedora/RHEL via Remi |
| M3 | Alpine/openSUSE, integração transacional com nginx/Apache |
| M4 | pacotes `.deb`/`.rpm`/`.apk`, i18n, CI completo |

Detalhes, critérios de aceite e o protocolo do helper privilegiado estão em
[docs/ROADMAP.md](docs/ROADMAP.md).

## Contribuindo

- Rode `make check` antes de abrir um pull request.
- Use commits convencionais (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `ci:`).
- Parsers novos precisam de teste com fixture; nada de depender da máquina local.
- Detecção deve continuar funcionando sem root e sem efeitos colaterais.

## Licença

MIT — veja [LICENSE](LICENSE).
