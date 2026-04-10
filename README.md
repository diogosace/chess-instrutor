# Chess Trainer (CLI) + Stockfish — Guia de Uso e do **DICA‑CÓDIGO**

Este projeto é um treinador de xadrez em terminal (CLI) feito em Python com `python-chess` + Stockfish.
Ele serve para:

- Você jogar uma partida (Brancas ou Pretas) contra a engine.
- Receber uma **classificação do seu lance** (estilo chess.com, com `?!`, `?`, `??`, `!`, `!!`, `#`).
- Ver linhas principais (PVs), alternativas melhores e respostas do adversário.
- Ver **Perigos** (armadilhas/punições concretas) caso alguém “se descuide”.
- Treinar a tomada de decisão no tabuleiro físico usando um resumo treinável: o **DICA‑CÓDIGO**.

> Observação: este README descreve o comportamento atual do script `board.py`.

---

## Como rodar

1. Garanta que você tem Python 3 instalado.
2. Instale `python-chess` (se ainda não tiver):

```bash
pip install python-chess
```

3. Rode:

```bash
python3 board.py
```

O Stockfish está apontado por padrão para um binário local (ajuste no `ENGINE_PATH` dentro de `board.py` se necessário).

---

## Fluxo do programa (o que aparece na tela)

### 1) Escolha de cor

No início, o programa pergunta:

- `Jogar de brancas ou pretas? [b/p]`

Se você escolher Pretas, a engine joga o primeiro lance.

### 2) Legenda do DICA‑CÓDIGO

O programa imprime uma vez:

- **Código de dica (legenda)**

Isso define o significado de cada letra/número do DICA‑CÓDIGO.

### 3) Seu lance

Quando é sua vez, você digita em SAN (ex.: `e4`, `Nf3`, `O-O`).

O programa então:

- Avalia o seu lance.
- Imprime a classificação (`?!`, `?`, `??`, `!`, `!!`, `#`).
- Mostra linhas principais e perigos.
- Mostra respostas interessantes do adversário.
- A engine joga.

### 4) Antes do seu próximo lance: **DICA‑CÓDIGO**

Depois do lance da engine, **antes de pedir seu lance**, o programa imprime uma linha curta:

```
DICA‑CÓDIGO: C? X? ? H? Δ?
```

Esse é o seu “painel de instrumentos” para treinar a decisão **no tabuleiro**, sem revelar automaticamente o melhor lance.

> Importante: por padrão o DICA‑CÓDIGO NÃO mostra “Melhor: …”.
> Existe um toggle no código (`SHOW_HINT_BEST_MOVE`) para reativar isso se você quiser mais tarde.

---

## Notas de lances (classificação)

O programa cola a nota no lance, quando faz sentido.
Para reduzir poluição visual:

- A nota `★` (melhor) **não é exibida**. Lance bom aparece “limpo”.
- As outras notas continuam (ex.: `?!`, `?`, `??`, `!`, `!!`, `#`).

Na prática:

- `??` = gafe (blunder)
- `?`  = erro (mistake)
- `?!` = imprecisão
- `!`  = ótimo
- `!!` = brilhante (heurística: sacrifício + robustez)
- `#`  = mate

---

## Perigos (armadilhas/punições)

Em cada variante principal, o programa tenta encontrar **perigos concretos**:

- Até **5 perigos** por seção.
- Cada perigo pode ter até **3 sub‑variantes** (`↳`) — sem “sub‑sub” para não poluir.

A ideia é você ver, fora da linha “ótima”, quais descuidos comuns permitem táticas/mate/ganhos.

### “Um lance que põe tudo a perder” (inevitável)

Quando a engine detecta que um descuido cria uma consequência realmente inevitável, o programa pode imprimir a linha completa.
Isso inclui:

- **Mate forçado**
- **Ganho forçado robusto** (vantagem grande que se mantém mesmo contra as melhores defesas)

---

# O que é o **DICA‑CÓDIGO**

O DICA‑CÓDIGO não é um “texto explicativo”.
Ele é uma **regra de priorização** para guiar seu cálculo humano de forma consistente.

Em vez de “te dizer o lance”, ele te diz:

- **que tipo de lance** o melhor lance tende a ser (cheque? captura?)
- **se há urgência tática** (mate a favor/contra)
- **se você está deixando material pendurado**
- **se a posição é crítica** (diferença grande entre melhor e 2º melhor)

Isso é útil porque o cálculo humano funciona bem quando você reduz o espaço de busca.

---

## Campos do DICA‑CÓDIGO (significado)

Exemplo:

```
DICA‑CÓDIGO: C0 X1 M3 H2 Δ120
```

### `C` — Cheque

- `C1` significa: o melhor lance (ou o caminho ótimo) é **um cheque**.
- `C0` significa: o melhor lance não é cheque (ou não é o mais importante agora).

**Como usar no tabuleiro (tradução casa‑peça)**:

- Se `C1`, você começa listando **todos os cheques legais**.
- Cada cheque vira uma “tradução casa‑peça” imediatamente:
  - “Bispo c4→f7”, “Dama h5→f7”, “Cavalo e5→f7”…

Depois você elimina cheques ruins (que perdem peça, que não continuam, etc.).

### `X` — Captura

- `X1` significa: o melhor lance é **uma captura**.
- `X0` significa: o melhor lance provavelmente não é captura imediata.

**Como usar no tabuleiro**:

- Se `X1`, você lista as capturas que:
  - ganham material,
  - criam ameaça forte,
  - ou resolvem um problema urgente.

Cada captura vira “casa‑peça”:

- “Cavalo f3→e5 captura”, “Bispo g2→c6 captura”, etc.

### `M` / `m` — Mate

- `M3` = mate a favor em 3 (prioridade máxima)
- `m4` = mate contra você em 4 (prioridade máxima defensiva)
- `-` = nada detectado nesse instante

**Como usar no tabuleiro**:

- Com `M…`: procure sequência forçante (cheques/capturas/sacrifícios) que encurta o mate.
- Com `m…`: procure o lance que **quebra a linha** (fuga do rei, interposição, captura do atacante, etc.).

Aqui você não está “adivinhando”; você está reduzindo o problema:

- “Eu preciso encontrar 1 defesa que remove o mate”

### `H` — Penduradas (aprox.)

`H` é uma contagem simples de peças suas que estão mais atacadas do que defendidas.

- `H0` = você não está pendurando nada óbvio (bom)
- `H2` = tem pelo menos duas coisas que podem cair se você ignorar

**Como usar no tabuleiro**:

- Se `H` é alto, sua lista de candidatos deve começar por:
  - defender,
  - mover a peça atacada,
  - trocar,
  - ou criar contra‑ameaça forçante (apenas se `Δ` alto e isso for realmente crítico).

### `Δ` — Criticidade (diferença entre 1º e 2º lance)

`Δ` é a diferença (em centipawns) entre a melhor opção e a segunda melhor.

- `Δ` baixo (ex.: 5–25): a posição é “calma”/flexível; vários lances são jogáveis.
- `Δ` alto (ex.: 80+): posição crítica — frequentemente existe um lance que evita desastre ou ganha muito.

**Como usar no tabuleiro**:

- `Δ` alto te diz: “não jogue no automático”.
- Ele muda seu comportamento: você deve calcular e confirmar.

---

# Como converter DICA‑CÓDIGO em **tradução casa‑peça** (método)

A conversão não é “o código vira um lance”.
A conversão é: o código vira uma **ordem de busca**, e dessa busca saem 2–3 lances candidatos em casa‑peça.

Use este algoritmo SEMPRE:

1) Leia `M/m`.
- Se existe mate (a favor ou contra), foque 100% nisso.

2) Leia `Δ`.
- Se `Δ` é alto, trate como posição crítica.

3) Se `C1`: gere todos os cheques → escreva mentalmente em casa‑peça.

4) Se `X1`: gere capturas candidatas → escreva mentalmente em casa‑peça.

5) Leia `H`.
- Se `H` alto, remova candidatos que deixam peça cair “de graça”.

6) Escolha 2–3 candidatos finais (casa‑peça) e só então confira suas variantes no programa.

---

## “Por que eu devo confiar na minha tradução?”

Você não está tentando replicar a árvore do Stockfish.
Você está treinando uma habilidade real: **priorização de candidatos**.

Por que isso funciona em posições desconhecidas:

- Cheques e capturas são os lances mais “forçantes” — eles reduzem respostas do oponente.
- `Δ` alto indica que “errar o tipo de lance” costuma custar caro.
- `H` alto indica que sua posição tem fraquezas concretas que o oponente pode punir.

Quando você usa o DICA‑CÓDIGO, você cria um hábito:

- primeiro buscar o que é forçante/urgente,
- depois cuidar de material/segurança,
- e só então planejar.

Isso é um padrão treinável, transferível para o tabuleiro físico.

A “confiança” não é fé: é consistência do processo.
Mesmo quando você errar o lance exato, você estará errando **dentro do conjunto certo de candidatos**.
Com repetição + checagem pelo programa, seu conjunto converge.

---

## Ajustes rápidos (se quiser mudar depois)

Dentro de `board.py` você encontra:

- `SHOW_HINT_CODE`: liga/desliga DICA‑CÓDIGO
- `SHOW_HINT_BEST_MOVE`: mostra/oculta a linha “Melhor: …”
- `ANALYSIS_TIME_HINT`: tempo da análise do DICA‑CÓDIGO
- `MAX_DANGERS`, `MAX_DANGER_SUBS`: limite de perigos e sub‑variantes

---

## Dica de treino (sugestão prática)

1. Jogue no tabuleiro físico.
2. Antes de mexer a peça, leia o DICA‑CÓDIGO e gere 2–3 candidatos (casa‑peça).
3. Só então digite o seu lance no programa.
4. Compare com PVs e Perigos.
5. Repetição (10–20 posições por dia) = aprendizado rápido.
