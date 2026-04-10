import chess
import chess.engine
import os
from typing import Optional, Dict, Tuple, Any, List

MATE_SCORE = 100000
ANALYSIS_TIME_MOVE = 1.0
ANALYSIS_TIME_SUGGEST = 1.2
SUGGEST_MULTIPV = 5

# Anotação de notas dentro das PVs (por ply).
# Mantém rápido: uma análise leve por posição, com cache.
ANALYSIS_TIME_PV_PLY = 0.05
PV_ANALYSE_CACHE_MAX = 4000

# Código didático (resumo treinável da posição)
SHOW_HINT_CODE = True
ANALYSIS_TIME_HINT = 0.12
SHOW_HINT_BEST_MOVE = False

# =========================
# UI / Estudo (terminal)
# =========================
STUDY_MODE = True
USE_EMOJIS = False
SHOW_MAIN_LINE = 5
SHOW_BETTER_MOVES = 5
SHOW_OPPONENT_BEST = 5
SHOW_OPPONENT_ERRORS = 2
SHOW_OPPONENT_IMPRECISIONS = 1
PV_PLIES_MAIN = 8

# Perigos (armadilhas/punições) exibidos por seção.
MAX_DANGERS = 5

# Sub-variantes de cada perigo (sem sub-subvariantes)
MAX_DANGER_SUBS = 3

# Reduz poluição: não repete exatamente o mesmo perigo em várias opções.
DEDUP_DANGERS_ACROSS_LINES = True

# Preview: como punir se o adversário NÃO jogar as melhores defesas.
OPPONENT_MISTAKES_PREVIEW_MAX = 3
OPPONENT_MISTAKES_PREVIEW_MIN_CP_LOSS = 80
OPPONENT_MISTAKES_PREVIEW_MIN_WP_LOSS = 0.030

# Quando a punição é "inevitável" (vantagem robusta mesmo com as melhores defesas)
FORCED_ADV_CP = 600

USE_COLORS = True
STYLE_DIM = False
STYLE_BOLD_HEADERS = True
PV_WITH_NUMBERS = True

ENGINE_PATH = "/Users/diogocerqueira/Desktop/Chess/stockfish/stockfish-macos-x86-64-bmi2"

board = chess.Board()
engine: Optional[chess.engine.SimpleEngine] = None

# Cache simples: (fen, time_s) -> info (dict de analyse)
_ANALYSE_CACHE: Dict[Tuple[str, float], Dict[str, Any]] = {}

# Cor do jogador humano (definida no início do programa).
HERO_COLOR: chess.Color = chess.WHITE


def configurar_stockfish_rapido(engine: chess.engine.SimpleEngine) -> None:
    """Força máxima (sem limitar Elo), mas com opções conservadoras pro hardware."""

    cpu = os.cpu_count() or 2
    # Em Macs antigos, usar todos os threads costuma aquecer/engasgar o sistema.
    threads = max(1, min(4, cpu - 1))

    # Hash pequeno/moderado evita swapping e mantém responsivo.
    # (Stockfish aceita em MB.)
    hash_mb = 128 if cpu <= 4 else 256

    options = {
        "Threads": threads,
        "Hash": hash_mb,
        "Ponder": "false",
        "Skill Level": 20,
        "UCI_LimitStrength": "false",
        # MultiPV é controlado por multipv=... no analyse, mas deixar default não atrapalha.
        "MultiPV": SUGGEST_MULTIPV,
    }

    try:
        engine.configure(options)
    except Exception:
        # Se alguma opção não existir na versão/binário, ignore silenciosamente.
        pass


def get_engine() -> chess.engine.SimpleEngine:
    global engine
    if engine is None:
        engine = chess.engine.SimpleEngine.popen_uci(ENGINE_PATH)
        configurar_stockfish_rapido(engine)
    return engine


def _shutdown_engine() -> None:
    global engine
    if engine is None:
        return
    try:
        engine.quit()
    except Exception:
        # fallback: em alguns cenários o quit pode falhar/travar; força o fechamento.
        try:
            engine.close()
        except Exception:
            pass
    finally:
        engine = None

moves_san = []
avaliacoes = []
    
def win_prob_from_cp(cp: int) -> float:
    # Mapeamento suave (0..1). Ajuda a classificar melhor em posições já ganhas/perdidas.
    # Constante escolhida para não “explodir” com pequenas variações.
    k = 0.0026
    try:
        import math

        # clamp pra evitar overflow numérico com mates convertidos
        cp = max(-MATE_SCORE, min(MATE_SCORE, int(cp)))
        return 1.0 / (1.0 + math.exp(-k * cp))
    except Exception:
        return 0.5


def classificar_lance(cp_loss: int, wp_loss: Optional[float] = None) -> str:
    cp_loss = max(0, int(cp_loss))
    if wp_loss is None:
        # fallback (compatibilidade) se chamarem só com centipawns
        if cp_loss <= 15:
            return "★"   # Best
        if cp_loss <= 50:
            return "!"   # Great
        if cp_loss <= 120:
            return "?!"  # Inaccuracy
        if cp_loss <= 250:
            return "?"   # Mistake
        return "??"      # Blunder

    wp_loss = max(0.0, float(wp_loss))

    # Critério híbrido (centipawn loss + queda de win-prob)
    # Ajustado pra ficar mais perto do “feeling” do chess.com.
    if cp_loss <= 15 or wp_loss <= 0.010:
        return "★"
    if cp_loss <= 50 or wp_loss <= 0.025:
        return "!"
    if cp_loss <= 120 or wp_loss <= 0.055:
        return "?!"
    if cp_loss <= 250 or wp_loss <= 0.120:
        return "?"
    return "??"

def imprimir_partida(moves_san, avaliacoes, now=True):
    saida = []
    for i in range(0, len(moves_san), 2):
        lance_num = i // 2 + 1

        branco = moves_san[i]
        branco_eval = avaliacoes[i] if i < len(avaliacoes) else ""
        branco_txt = _annotate_move(branco, branco_eval)

        if i + 1 < len(moves_san):
            preto = moves_san[i+1]
            preto_eval = avaliacoes[i+1]
            preto_txt = _annotate_move(preto, preto_eval)
            saida.append(f"{lance_num}. {branco_txt} {preto_txt}")
        else:
            saida.append(f"{lance_num}. {branco_txt}")
    print(f"\n📜 {' '.join(saida)}")


def _annotate_move(san: str, grade: str) -> str:
    """Retorna SAN com a nota colada no lance (ex.: d4!; Nf3★).

    Evita duplicar mate: se o SAN já termina com '#', não anota com '#'.
    """
    grade = (grade or "").strip()
    if not grade:
        return san
    # Para reduzir poluição visual: não imprime a estrelinha nos lances bons.
    if grade == "★":
        return san
    # Se o SAN já é mate, não polui com nota.
    if san.endswith("#"):
        return san
    if grade == "#":
        return san
    return f"{san}{grade}"


def _grade_for_display(grade: str) -> str:
    """Retorna sufixo de nota para prints (omitir ★)."""
    g = (grade or "").strip()
    if not g or g == "★":
        return ""
    return g


def _square_name(sq: int) -> str:
    try:
        return chess.square_name(sq)
    except Exception:
        return str(sq)


def _piece_name_pt(piece_type: int) -> str:
    return {
        chess.PAWN: "Peão",
        chess.KNIGHT: "Cavalo",
        chess.BISHOP: "Bispo",
        chess.ROOK: "Torre",
        chess.QUEEN: "Dama",
        chess.KING: "Rei",
    }.get(piece_type, "Peça")


def _move_piece_from_to(board_pos: chess.Board, m: chess.Move) -> str:
    p = board_pos.piece_at(m.from_square)
    if p is None:
        return f"{_square_name(m.from_square)}→{_square_name(m.to_square)}"
    return f"{_piece_name_pt(p.piece_type)} {_square_name(m.from_square)}→{_square_name(m.to_square)}"


def _hanging_count_simple(board_pos: chess.Board, color: chess.Color) -> int:
    """Contagem simples de peças "penduradas" (aprox) para treino visual."""
    try:
        opp = not color
        cnt = 0
        for sq, p in board_pos.piece_map().items():
            if p.color != color:
                continue
            if p.piece_type == chess.KING:
                continue
            atk = len(board_pos.attackers(opp, sq))
            if atk <= 0:
                continue
            dfn = len(board_pos.attackers(color, sq))
            if atk > dfn:
                cnt += 1
        return cnt
    except Exception:
        return 0


def _hint_code_legend() -> None:
    titulo("Código de dica (legenda)")
    print("- C: melhor lance dá cheque (1/0)")
    print("- X: melhor lance é captura (1/0)")
    print("- M: mate a favor em N (ex.: M3) | m: mate contra em N (ex.: m4)")
    print("- H: nº de peças suas penduradas (aprox)")
    print("- Δ: diferença (melhor vs 2º melhor) em cp; alto = posição crítica/" "lance único")


def _print_hint_code(board_pos: chess.Board) -> None:
    """Imprime um código curto e treinável para a posição atual (lado a jogar)."""
    if not SHOW_HINT_CODE:
        return
    if board_pos.is_game_over():
        return

    try:
        infos = analyse_sorted(board_pos, time_s=ANALYSIS_TIME_HINT, multipv=2, pov_color=board_pos.turn)
        if not infos or not infos[0].get("pv"):
            return

        best = infos[0]
        pv1 = best.get("pv", [])
        m1 = pv1[0]
        if m1 not in board_pos.legal_moves:
            return

        cp1 = score_to_cp(best["score"], board_pos.turn)
        mate_pov = best["score"].pov(board_pos.turn).mate() if best["score"].pov(board_pos.turn).is_mate() else None

        cp2 = None
        if len(infos) > 1 and infos[1].get("score") is not None:
            cp2 = score_to_cp(infos[1]["score"], board_pos.turn)
        delta = None if cp2 is None else int(cp1 - cp2)

        C = 0
        X = 0
        try:
            C = 1 if board_pos.gives_check(m1) else 0
        except Exception:
            C = 0
        try:
            X = 1 if board_pos.is_capture(m1) else 0
        except Exception:
            X = 0

        H = _hanging_count_simple(board_pos, board_pos.turn)

        mate_tag = "-"
        if mate_pov is not None:
            if mate_pov > 0:
                mate_tag = f"M{mate_pov}"
            elif mate_pov < 0:
                mate_tag = f"m{-mate_pov}"

        delta_txt = "-" if delta is None else str(delta)

        try:
            san = board_pos.san(m1)
        except Exception:
            san = str(m1)

        print(f"\n{_label('DICA-CÓDIGO:')} C{C} X{X} {mate_tag} H{H} Δ{delta_txt}")
        if SHOW_HINT_BEST_MOVE:
            traduz = _move_piece_from_to(board_pos, m1)
            print(f"{_label('Melhor:')} {traduz} | {san}")
    except Exception:
        return

def desc_qualidade(avaliacao):
    if avaliacao == "?!":
        return "Imprecisão"
    if avaliacao == "?":
        return "Erro"
    if avaliacao == "#":
        return "Xeque-Mate"
    if avaliacao == "!!":
        return "Brilhante"
    if avaliacao == "!":
        return "Ótimo"
    if avaliacao == "★":
        return "Melhor"
    if avaliacao == "??":
        return "Gafe"
    return "Indisponível"


def titulo(txt: str) -> None:
    print(f"\n{txt}")


def _style_dim(text: str) -> str:
    if not USE_COLORS or not STYLE_DIM:
        return text
    return f"\033[2m{text}\033[0m"


def _style_bold(text: str) -> str:
    if not USE_COLORS or not STYLE_BOLD_HEADERS:
        return text
    return f"\033[1m{text}\033[0m"


def _label(label: str) -> str:
    # Negrito leve só no prefixo (não grita, mas ajuda fixação)
    return _style_bold(label)

def score_to_cp(score: chess.engine.PovScore, pov_color: chess.Color) -> int:
    s = score.pov(pov_color)
    if s.is_mate():
        mate = s.mate()
        if mate is None:
            return 0
        if mate > 0:
            return MATE_SCORE - mate
        return -MATE_SCORE - mate

    cp = s.score()
    return 0 if cp is None else int(cp)


def score_to_int_turn(score, board):
    # Mantido para compatibilidade com partes antigas: “valor” sempre do lado a jogar.
    return score_to_cp(score, board.turn)


def analyse_sorted(board: chess.Board, *, time_s: float, multipv: int, pov_color: chess.Color):
    info = get_engine().analyse(board, chess.engine.Limit(time=time_s), multipv=multipv)
    info.sort(key=lambda x: score_to_cp(x["score"], pov_color), reverse=True)
    return info


def analyse_cached_single(board: chess.Board, *, time_s: float) -> Dict[str, Any]:
    """Análise leve (multipv=1) com cache por FEN.

    Retorna sempre um dict (mesmo se a engine devolver lista com 1 entrada).
    """
    fen = board.fen()
    key = (fen, float(time_s))
    cached = _ANALYSE_CACHE.get(key)
    if cached is not None:
        return cached

    info = get_engine().analyse(board, chess.engine.Limit(time=time_s), multipv=1)
    if isinstance(info, list):
        info0: Dict[str, Any] = info[0] if info else {"score": chess.engine.PovScore(chess.engine.Cp(0), board.turn)}
    else:
        info0 = info

    if len(_ANALYSE_CACHE) >= PV_ANALYSE_CACHE_MAX:
        _ANALYSE_CACHE.clear()
    _ANALYSE_CACHE[key] = info0
    return info0


def material_balance(board: chess.Board, pov_color: chess.Color) -> int:
    values = {
        chess.PAWN: 1,
        chess.KNIGHT: 3,
        chess.BISHOP: 3,
        chess.ROOK: 5,
        chess.QUEEN: 9,
        chess.KING: 0,
    }

    def side_material(color: chess.Color) -> int:
        total = 0
        for piece_type, value in values.items():
            total += len(board.pieces(piece_type, color)) * value
        return total

    return side_material(pov_color) - side_material(not pov_color)


def detectar_brilhante_por_sacrificio(
    board_before: chess.Board,
    move_obj: chess.Move,
    played_info_after: dict,
    played_cp: int,
    grade: str,
) -> bool:
    # Heurística pragmática (aproxima chess.com):
    # - lance é (quase) o melhor
    # - e a linha principal do engine mostra que você fica materialmente pior (sacrifício)
    # - mas mantém grande vantagem ou mate.
    if grade not in ("★", "!"):
        return False

    pov = board_before.turn
    score_obj = played_info_after.get("score")
    if score_obj is None:
        return False

    pov_score = score_obj.pov(pov)
    is_mate_for_pov = pov_score.is_mate() and (pov_score.mate() or 0) > 0
    if not is_mate_for_pov and played_cp < 200:
        return False

    pv = played_info_after.get("pv", [])
    if not pv:
        return False

    board_after = board_before.copy()
    board_after.push(move_obj)

    temp = board_after.copy()
    for m in pv[:2]:
        if m in temp.legal_moves:
            temp.push(m)
        else:
            break

    bal0 = material_balance(board_before, pov)
    bal1 = material_balance(temp, pov)

    # “sacrifício” = pelo menos uma peça menor (>= 3) de diferença na linha principal
    return bal1 <= bal0 - 3


def pv_em_san(board: chess.Board, pv, max_plies: int = 10) -> str:
    temp = board.copy()
    out = []
    for m in (pv or [])[:max_plies]:
        try:
            out.append(temp.san(m))
        except Exception:
            out.append(str(m))

        if m in temp.legal_moves:
            temp.push(m)
        else:
            break
    return " ".join(out)


def pv_em_san_numbered(board: chess.Board, pv, max_plies: int = 10) -> str:
    """PV em SAN com numeração estilo PGN, sem repetir '2...' em toda jogada.

    Exemplos:
    - Se é a vez das Pretas: "1... e5 2. Nf3 Nc6 3. d4 exd4"
    - Se é a vez das Brancas: "1. d4 d5 2. c4 e6"
    """
    temp = board.copy()
    out = []

    for m in (pv or [])[:max_plies]:
        try:
            san = temp.san(m)
        except Exception:
            san = str(m)

        if temp.turn == chess.WHITE:
            out.append(f"{temp.fullmove_number}. {san}")
        else:
            # Só marca com "..." se é o PRIMEIRO lance mostrado e começa com as pretas.
            if not out:
                out.append(f"{temp.fullmove_number}... {san}")
            else:
                out.append(san)

        if m in temp.legal_moves:
            temp.push(m)
        else:
            break

    return " ".join(out)


def pv_em_san_numbered_graded(board: chess.Board, pv, grade: str, max_plies: int = 10) -> str:
    """PV em SAN com numeração + nota colada apenas no 1º lance da linha.

    Importante: não faz chamadas extras de engine.
    A nota representa a qualidade da VARIANTE (o 1º lance), sem poluir os plies seguintes.
    Ex.: "1... d6! 2. d4 d5 3. Nc3 ..."
    """
    temp = board.copy()
    out = []

    first = True
    for m in (pv or [])[:max_plies]:
        try:
            san = temp.san(m)
        except Exception:
            san = str(m)

        token = _annotate_move(san, grade) if first else san

        if temp.turn == chess.WHITE:
            out.append(f"{temp.fullmove_number}. {token}")
        else:
            if not out:
                out.append(f"{temp.fullmove_number}... {token}")
            else:
                out.append(token)

        if m in temp.legal_moves:
            temp.push(m)
        else:
            break

        first = False
    return " ".join(out)


def _grades_for_pv(board: chess.Board, pv, *, max_plies: int, time_s: float, first_grade_override: Optional[str] = None) -> List[str]:
    """Calcula uma nota por ply, comparando o lance jogado na PV com o melhor lance da posição.

    Importante: isto não explora alternativas em cada nó (isso seria MultiPV recursivo e caro).
    Ele mede apenas “o quão bom é o lance escolhido” vs. o melhor, em cada posição da PV.
    """
    temp = board.copy()
    moves: List[chess.Move] = []
    positions: List[chess.Board] = [temp.copy()]

    for m in (pv or [])[:max_plies]:
        if m not in temp.legal_moves:
            break
        moves.append(m)
        temp.push(m)
        positions.append(temp.copy())

    if not moves:
        return []

    infos = [analyse_cached_single(pos, time_s=time_s) for pos in positions]
    grades: List[str] = []

    for i in range(len(moves)):
        mover = positions[i].turn
        best_score = infos[i].get("score")
        after_score = infos[i + 1].get("score")
        if best_score is None or after_score is None:
            grades.append("")
            continue

        best_cp = score_to_cp(best_score, mover)
        played_cp = score_to_cp(after_score, mover)
        cp_loss = max(0, best_cp - played_cp)
        wp_loss = max(0.0, win_prob_from_cp(best_cp) - win_prob_from_cp(played_cp))

        grade = classificar_lance(cp_loss, wp_loss)

        # Regras duras (mates) para ficar com cara de chess.com em táticas diretas.
        try:
            s_best = best_score.pov(mover)
            s_after = after_score.pov(mover)

            # Permitiu mate contra você rápido? -> gafe
            if s_after.is_mate():
                m_after = s_after.mate()
                if m_after is not None and m_after < 0 and (-m_after) <= 6:
                    grade = "??"

            # Perdeu mate forçado curto que você tinha? -> gafe
            if s_best.is_mate():
                bm = s_best.mate()
                if bm is not None and bm > 0 and bm <= 4:
                    pm = s_after.mate() if s_after.is_mate() else None
                    if pm is None or pm <= 0 or pm > bm:
                        grade = "??"
        except Exception:
            pass

        grades.append(grade)

    if first_grade_override and grades:
        grades[0] = first_grade_override

    return grades


def pv_em_san_numbered_autograded(board: chess.Board, pv, *, max_plies: int = 10, first_grade_override: Optional[str] = None) -> str:
    """PV em SAN com numeração + nota em CADA lance mostrado.

    A nota de cada ply é calculada localmente (melhor vs. escolhido na PV).
    Para consistência, pode-se forçar a nota do 1º lance (ex.: nota da variante no MultiPV da raiz).
    """
    grades = _grades_for_pv(board, pv, max_plies=max_plies, time_s=ANALYSIS_TIME_PV_PLY, first_grade_override=first_grade_override)

    temp = board.copy()
    out: List[str] = []
    idx = 0
    for m in (pv or [])[:max_plies]:
        if m not in temp.legal_moves:
            break
        try:
            san = temp.san(m)
        except Exception:
            san = str(m)

        grade = grades[idx] if idx < len(grades) else ""
        token = _annotate_move(san, grade)

        if temp.turn == chess.WHITE:
            out.append(f"{temp.fullmove_number}. {token}")
        else:
            if not out:
                out.append(f"{temp.fullmove_number}... {token}")
            else:
                out.append(token)

        temp.push(m)
        idx += 1

    return " ".join(out)


def fmt_cp(cp: int) -> str:
    if abs(cp) >= MATE_SCORE - 1000:
        return "mate"
    s = f"{cp/100:.2f}"
    return f"{s}".replace("-0.00", "0.00")


def ajustar_nota_abertura(board_before: chess.Board, grade: str, cp_loss: int) -> str:
    # No começo da partida, Stockfish pode variar levemente entre lances “de livro”.
    # Pra treino/repertório, tratamos pequenas perdas como "★".
    ply = len(board_before.move_stack)
    if ply <= 8 and grade == "!" and cp_loss <= 35:
        return "★"
    return grade


def _refinar_classificacao_se_fronteira(
    board_before: chess.Board,
    board_after: chess.Board,
    *,
    pov: chess.Color,
    best_cp: int,
    best_wp: float,
    played_cp: int,
    grade: str,
    cp_loss: int,
    wp_loss: float,
) -> Tuple[str, int, float, int]:
    """Reduz oscilação do grading perto dos limiares (ex.: ?! vs ?).

    Só re-analisa quando está "na borda" e no começo da partida.
    Retorna: (grade, best_cp, best_wp, played_cp) possivelmente refinados.
    """

    try:
        ply = len(board_before.move_stack)
        if ply > 10:
            return grade, best_cp, best_wp, played_cp

        # Zona de fronteira entre ?! e ? (pelos thresholds atuais).
        borderline_cp = 95 <= int(cp_loss) <= 170
        borderline_wp = 0.040 <= float(wp_loss) <= 0.085

        if grade not in ("?!", "?"):
            return grade, best_cp, best_wp, played_cp
        if not (borderline_cp or borderline_wp):
            return grade, best_cp, best_wp, played_cp

        # Re-analisa com mais tempo só nesses casos.
        t = max(1.6, float(ANALYSIS_TIME_MOVE) * 2.0)

        best_info2 = analyse_sorted(board_before, time_s=t, multipv=3, pov_color=pov)
        played_info2 = get_engine().analyse(board_after, chess.engine.Limit(time=t))

        best_cp2 = score_to_cp(best_info2[0]["score"], pov)
        best_wp2 = win_prob_from_cp(best_cp2)
        played_cp2 = score_to_cp(played_info2["score"], pov)

        cp_loss2 = max(0, best_cp2 - played_cp2)
        wp_loss2 = max(0.0, best_wp2 - win_prob_from_cp(played_cp2))
        grade2 = classificar_lance(cp_loss2, wp_loss2)

        return grade2, best_cp2, best_wp2, played_cp2
    except Exception:
        return grade, best_cp, best_wp, played_cp


def _heuristica_fraqueza_rei_minima(board_before: chess.Board, move_obj: chess.Move, grade: str) -> str:
    """Heurística pequena e intencional para treino.

    Alguns lances de abertura criam fraquezas clássicas (ex.: f-pawn 1 passo), e o
    Stockfish pode oscilar entre ?! e ? com pouco tempo. Aqui forçamos um piso.
    """
    try:
        ply = len(board_before.move_stack)
        if ply > 2:
            return grade

        uci = move_obj.uci()
        if uci in ("f2f3", "f7f6"):
            if grade in ("★", "!", "?!"):
                return "?"
        return grade
    except Exception:
        return grade


def _piece_value(piece_type: int) -> int:
    return {
        chess.PAWN: 1,
        chess.KNIGHT: 3,
        chess.BISHOP: 3,
        chess.ROOK: 5,
        chess.QUEEN: 9,
        chess.KING: 0,
    }.get(piece_type, 0)


def _mate_in_one(board_pos: chess.Board) -> Optional[chess.Move]:
    """Retorna um lance que dá mate imediatamente (se existir), sem engine."""
    for m in board_pos.legal_moves:
        try:
            after = board_pos.copy()
            after.push(m)
            if after.is_checkmate():
                return m
        except Exception:
            continue
    return None


def _blunder_priority(board_pos: chess.Board, m: chess.Move, *, hero_color: chess.Color) -> Tuple[int, str]:
    """Ordena lances "descuidos" prováveis: empurrões de peão do roque (f/g/h) vêm primeiro."""
    try:
        p = board_pos.piece_at(m.from_square)
        if p is None or p.color != hero_color:
            return (50, m.uci())
        if p.piece_type == chess.PAWN:
            file = chess.square_file(m.from_square)
            # f=5, g=6, h=7
            if file == 6:
                return (0, m.uci())
            if file == 5:
                return (1, m.uci())
            if file == 7:
                return (2, m.uci())
            # peões centrais também costumam abrir diagonais cedo
            if file in (3, 4):
                return (6, m.uci())
            return (12, m.uci())
        # movimentos de rei cedo também são descuido frequente
        if p.piece_type == chess.KING:
            return (8, m.uci())
        return (20, m.uci())
    except Exception:
        return (50, m.uci())


def _trap_mate_after_first_reply(board_start: chess.Board, pv, *, hero_color: chess.Color, max_blunders: int = 18) -> str:
    """Procura armadilha concreta: após o 1º lance do adversário (pv[0]), um descuido do herói permite mate em 1."""
    if not pv:
        return ""

    first = pv[0]
    b1 = board_start.copy()
    if first not in b1.legal_moves:
        return ""
    b1.push(first)

    # Agora é a vez do herói: procura um descuido plausível.
    if b1.turn != hero_color:
        return ""

    hero_moves = list(b1.legal_moves)
    hero_moves.sort(key=lambda m: _blunder_priority(b1, m, hero_color=hero_color))

    for hm in hero_moves[:max_blunders]:
        try:
            b2 = b1.copy()
            b2.push(hm)
            mate_move = _mate_in_one(b2)
            if mate_move is not None:
                # Linha: resposta do adversário, descuido, mate.
                seq = [first, hm, mate_move]
                return pv_em_san_numbered(board_start, seq, 3) if PV_WITH_NUMBERS else pv_em_san(board_start, seq, 3)
        except Exception:
            continue

    return ""


def _has_check_or_capture(board_pos: chess.Board) -> bool:
    """Filtro rápido: posição tem algum cheque ou captura disponível?"""
    try:
        for m in board_pos.legal_moves:
            if board_pos.is_capture(m):
                return True
            try:
                if board_pos.gives_check(m):
                    return True
            except Exception:
                pass
    except Exception:
        return False
    return False


def _extend_pv_until_mate(
    board_pos: chess.Board,
    pv_seed: List[chess.Move],
    *,
    max_plies: int,
    time_s: float,
) -> List[chess.Move]:
    """Tenta estender uma PV até mate (quando existir), mantendo baixo custo.

    Usado apenas em casos raros de 'um lance que põe tudo a perder' (mate forçado).
    """

    temp = board_pos.copy()
    out: List[chess.Move] = []

    for m in (pv_seed or []):
        if m not in temp.legal_moves:
            break
        out.append(m)
        temp.push(m)
        if temp.is_checkmate():
            return out

    while len(out) < max_plies and not temp.is_game_over():
        mate1 = _mate_in_one(temp)
        if mate1 is not None:
            out.append(mate1)
            temp.push(mate1)
            break

        info = analyse_cached_single(temp, time_s=time_s)
        pv = info.get("pv", [])
        if not pv:
            break
        m0 = pv[0]
        if m0 not in temp.legal_moves:
            break
        out.append(m0)
        temp.push(m0)
        if temp.is_checkmate():
            break

    return out


def _robust_advantage_after_move(
    board_before: chess.Board,
    punisher_move: chess.Move,
    *,
    punisher_color: chess.Color,
    min_cp: int,
    time_s: float,
    defenses: int = 3,
) -> bool:
    """Retorna True se, após `punisher_move`, a vantagem do punidor segue alta contra as melhores defesas."""

    try:
        b1 = board_before.copy()
        if punisher_move not in b1.legal_moves:
            return False
        b1.push(punisher_move)

        # Agora é a vez do "vítima" defender.
        infos = get_engine().analyse(b1, chess.engine.Limit(time=time_s), multipv=max(1, defenses))
        if isinstance(infos, dict):
            infos = [infos]

        worst = None
        for line in infos:
            pv = line.get("pv", [])
            if not pv:
                continue
            d0 = pv[0]
            if d0 not in b1.legal_moves:
                continue

            b2 = b1.copy()
            b2.push(d0)
            reply = analyse_cached_single(b2, time_s=time_s)
            score = reply.get("score")
            if score is None:
                continue
            cp = score_to_cp(score, punisher_color)
            worst = cp if worst is None else min(worst, cp)

        if worst is None:
            return False

        return worst >= int(min_cp)
    except Exception:
        return False


def _severity_key(*, mate_in: Optional[int], cp: Optional[int]) -> Tuple[int, int]:
    """Ordena perigos: mates forçados primeiro (menor mate_in), depois maior vantagem em cp."""
    if mate_in is not None and mate_in > 0:
        return (0, int(mate_in))
    # sem mate: ordena por cp (maior melhor). Usamos negativo para ficar ascendente.
    return (1, -int(cp or 0))


def danger_variants_grouped(
    board_start: chess.Board,
    pv,
    *,
    victim_color: chess.Color,
    start_after_plies: int,
    max_groups: int,
    max_subs: int,
    max_blunders: int = 18,
    engine_time_s: float = 0.08,
    min_cp_advantage: int = 250,
    pv_plies: int = 6,
    exclude_uci: Optional[set[str]] = None,
) -> List[Dict[str, Any]]:
    """Versão agrupada do explorador de perigos.

    Retorna lista de dicts: {"main": str, "subs": [str, ...]}.
    """

    base = board_start.copy()

    if start_after_plies:
        if not pv:
            return []
        applied = 0
        for m in (pv or [])[:start_after_plies]:
            if m not in base.legal_moves:
                break
            base.push(m)
            applied += 1
        if applied != start_after_plies:
            return []

    if base.turn != victim_color:
        return []

    candidates: List[Dict[str, Any]] = []
    forced_full: Optional[Tuple[Tuple[int, int], str]] = None

    blunders = list(base.legal_moves)
    blunders.sort(key=lambda m: _blunder_priority(base, m, hero_color=victim_color))

    for bm in blunders[:max_blunders]:
        uci = bm.uci()
        if exclude_uci and uci in exclude_uci:
            continue
        try:
            after_blunder = base.copy()
            after_blunder.push(bm)
        except Exception:
            continue

        # Mate em 1: completo e inevitável.
        mate1 = _mate_in_one(after_blunder)
        if mate1 is not None:
            line_moves = [bm, mate1]
            # Para mate em 1, a linha já é "completa" e curta; usamos a versão autogradeada
            # e NÃO duplicamos com uma versão sem notas.
            txt_forced = pv_em_san_numbered_autograded(base, line_moves, max_plies=len(line_moves)) if PV_WITH_NUMBERS else pv_em_san(base, line_moves, len(line_moves))
            key = _severity_key(mate_in=1, cp=MATE_SCORE)
            if forced_full is None or key < forced_full[0]:
                forced_full = (key, txt_forced)
            continue

        if not _has_check_or_capture(after_blunder):
            continue

        try:
            info = analyse_cached_single(after_blunder, time_s=engine_time_s)
            score = info.get("score")
            pv_punish = info.get("pv", [])
            if score is None or not pv_punish:
                continue

            punisher = after_blunder.turn
            cp_punisher = score_to_cp(score, punisher)
            pov = score.pov(punisher)
            mate_in = pov.mate() if pov.is_mate() else None
            is_mate = mate_in is not None and mate_in > 0

            punisher_first = pv_punish[0]
            # Threshold mais permissivo se a primeira punição é cheque/captura forte.
            thr = int(min_cp_advantage)
            try:
                if after_blunder.gives_check(punisher_first):
                    thr = min(thr, 120)
            except Exception:
                pass
            try:
                if after_blunder.is_capture(punisher_first):
                    cap = after_blunder.piece_at(punisher_first.to_square)
                    if cap is not None and _piece_value(cap.piece_type) >= 3:
                        thr = min(thr, 150)
            except Exception:
                pass

            if not is_mate and cp_punisher < thr:
                continue

            # Linha principal curta (com notas)
            follow = []
            temp = after_blunder.copy()
            for mm in pv_punish[: max(1, pv_plies - 1)]:
                if mm not in temp.legal_moves:
                    break
                follow.append(mm)
                temp.push(mm)
            line_moves = [bm] + follow
            txt_main = pv_em_san_numbered_autograded(base, line_moves, max_plies=len(line_moves)) if PV_WITH_NUMBERS else pv_em_san(base, line_moves, len(line_moves))

            # Para sub-variantes: posição após a 1ª punição (o "gancho" do perigo)
            after_punish = None
            try:
                ap = base.copy()
                ap.push(bm)
                ap.push(punisher_first)
                after_punish = ap
            except Exception:
                after_punish = None

            severity = _severity_key(mate_in=int(mate_in) if is_mate else None, cp=int(cp_punisher))
            candidates.append(
                {
                    "bm": bm,
                    "main": txt_main,
                    "subs": [],
                    "after_punish": after_punish,
                    "severity": severity,
                    "mate_in": int(mate_in) if is_mate else None,
                    "cp": int(cp_punisher),
                    "punisher_first": punisher_first,
                    "after_blunder": after_blunder,
                }
            )

            # Linha inevitável: mate forçado OU ganho forçado robusto.
            if is_mate:
                seed = []
                temp3 = after_blunder.copy()
                for mm in pv_punish:
                    if mm not in temp3.legal_moves:
                        break
                    seed.append(mm)
                    temp3.push(mm)
                    if temp3.is_checkmate():
                        break
                extended = _extend_pv_until_mate(after_blunder, seed, max_plies=32, time_s=max(0.05, engine_time_s))
                full_moves = [bm] + extended
                txt_forced = pv_em_san_numbered(base, full_moves, max_plies=len(full_moves)) if PV_WITH_NUMBERS else pv_em_san(base, full_moves, len(full_moves))
                k = _severity_key(mate_in=int(mate_in), cp=MATE_SCORE)
                if forced_full is None or k < forced_full[0]:
                    forced_full = (k, txt_forced)

            elif cp_punisher >= FORCED_ADV_CP:
                # Confirma robustez contra as melhores defesas ("não dá pra parar").
                if _robust_advantage_after_move(after_blunder, punisher_first, punisher_color=punisher, min_cp=FORCED_ADV_CP, time_s=max(0.06, engine_time_s), defenses=3):
                    # Mostra um pouco mais de PV para deixar o ganho claro.
                    more = []
                    temp4 = after_blunder.copy()
                    for mm in pv_punish[: min(len(pv_punish), max(pv_plies, 10))]:
                        if mm not in temp4.legal_moves:
                            break
                        more.append(mm)
                        temp4.push(mm)
                        if temp4.is_game_over():
                            break
                    full_moves = [bm] + more
                    txt_forced = pv_em_san_numbered(base, full_moves, max_plies=len(full_moves)) if PV_WITH_NUMBERS else pv_em_san(base, full_moves, len(full_moves))
                    k = _severity_key(mate_in=None, cp=cp_punisher)
                    if forced_full is None or k < forced_full[0]:
                        forced_full = (k, txt_forced)

        except Exception:
            continue

    # Ordena e limita.
    candidates.sort(key=lambda x: x.get("severity", (9, 0)))

    chosen = candidates[: max(0, int(max_groups))]

    # Sub-variantes: explora um 2º descuido após a 1ª punição.
    if max_subs > 0:
        for c in chosen:
            # Se o perigo principal já termina em mate, não precisa de sub-variantes.
            try:
                if "#" in str(c.get("main", "")):
                    c["subs"] = []
                    continue
            except Exception:
                pass
            ap: Optional[chess.Board] = c.get("after_punish")
            if ap is None:
                continue
            if ap.is_game_over():
                continue
            if ap.turn != victim_color:
                continue

            subs: List[str] = []
            sub_moves = list(ap.legal_moves)
            sub_moves.sort(key=lambda m: _blunder_priority(ap, m, hero_color=victim_color))
            for sm in sub_moves[: max(10, max_subs * 6)]:
                try:
                    bsub = ap.copy()
                    bsub.push(sm)
                except Exception:
                    continue

                # Mate em 1 na sequência
                m1 = _mate_in_one(bsub)
                if m1 is not None:
                    seq = [sm, m1]
                    txt = pv_em_san_numbered_autograded(ap, seq, max_plies=len(seq)) if PV_WITH_NUMBERS else pv_em_san(ap, seq, len(seq))
                    subs.append(txt)
                    if len(subs) >= max_subs:
                        break
                    continue

                if not _has_check_or_capture(bsub):
                    continue

                try:
                    info2 = analyse_cached_single(bsub, time_s=max(0.06, engine_time_s))
                    s2 = info2.get("score")
                    pv2 = info2.get("pv", [])
                    if s2 is None or not pv2:
                        continue
                    pun = bsub.turn
                    cp2 = score_to_cp(s2, pun)
                    mate2 = s2.pov(pun).mate() if s2.pov(pun).is_mate() else None
                    if (mate2 is None or mate2 <= 0) and cp2 < 220:
                        continue

                    follow2 = []
                    t2 = bsub.copy()
                    for mm in pv2[: max(1, pv_plies - 1)]:
                        if mm not in t2.legal_moves:
                            break
                        follow2.append(mm)
                        t2.push(mm)
                    seq = [sm] + follow2
                    txt = pv_em_san_numbered_autograded(ap, seq, max_plies=len(seq)) if PV_WITH_NUMBERS else pv_em_san(ap, seq, len(seq))
                    subs.append(txt)
                    if len(subs) >= max_subs:
                        break
                except Exception:
                    continue

            c["subs"] = subs

    out: List[Dict[str, Any]] = []
    if forced_full is not None:
        out.append({"main": forced_full[1], "subs": []})

    for c in chosen:
        out.append({"main": c.get("main", ""), "subs": c.get("subs", [])})

    # Remove vazios/duplicados preservando ordem.
    seen_txt: set[str] = set()
    final: List[Dict[str, Any]] = []
    for item in out:
        main = (item.get("main") or "").strip()
        if not main:
            continue
        if main in seen_txt:
            continue
        seen_txt.add(main)
        subs = [s for s in (item.get("subs") or []) if s and s not in seen_txt][: max(0, int(max_subs))]
        for s in subs:
            seen_txt.add(s)
        final.append({"main": main, "subs": subs})

    return final


def _print_dangers_grouped(
    dangers: List[Dict[str, Any]],
    *,
    label: str = "Perigo",
    global_seen: Optional[set[str]] = None,
) -> None:
    """Impressão compacta de perigos e sub-variantes.

    - Evita repetir o prefixo "Perigo:" em todas as linhas.
    - Opcionalmente deduplica perigos iguais via `global_seen`.
    """
    if not dangers:
        return

    seen = global_seen if global_seen is not None else set()
    first_printed = False

    for item in dangers:
        main = (item.get("main") or "").strip()
        if not main:
            continue
        if main in seen:
            continue

        prefix = f"  {_label(label + ':')} " if not first_printed else "  " + (" " * (len(label) + 2))
        print(f"{prefix}{main}")
        seen.add(main)
        first_printed = True

        for sub in (item.get("subs") or [])[:MAX_DANGER_SUBS]:
            sub = (sub or "").strip()
            if not sub:
                continue
            if sub in seen:
                continue
            print(f"    ↳ {sub}")
            seen.add(sub)


def danger_variants(
    board_start: chess.Board,
    pv,
    *,
    victim_color: chess.Color,
    start_after_plies: int,
    max_lines: int = 2,
    max_blunders: int = 14,
    engine_time_s: float = 0.08,
    min_cp_advantage: int = 250,
    pv_plies: int = 6,
    exclude_uci: Optional[set[str]] = None,
) -> List[str]:
    """Encontra "perigos" como punições concretas após um descuido plausível.

    - Aplica `pv[:start_after_plies]` para chegar na posição-alvo.
    - Gera alguns lances do lado `victim_color` que parecem "descuidos".
    - Para cada descuido, tenta achar punição:
      - mate em 1 (sem engine)
      - caso contrário, usa uma análise leve para confirmar uma tática forte (>= min_cp_advantage ou mate)

    Retorna linhas em SAN já numeradas, começando no ply ruim (não repete lances anteriores).
    """
    base = board_start.copy()
    if start_after_plies:
        if not pv:
            return []
        applied = 0
        for m in (pv or [])[:start_after_plies]:
            if m not in base.legal_moves:
                break
            base.push(m)
            applied += 1

        if applied != start_after_plies:
            return []

    if base.turn != victim_color:
        return []

    results: List[str] = []
    seen: set[str] = set()

    # Se existir uma linha de mate forçado após um descuido, mostramos ela completa
    # mesmo se passar do limite ("um lance que põe tudo a perder").
    forced_best: Optional[Tuple[int, str]] = None  # (mate_in, line_txt)

    blunders = list(base.legal_moves)
    blunders.sort(key=lambda m: _blunder_priority(base, m, hero_color=victim_color))

    for bm in blunders[:max_blunders]:
        key = bm.uci()
        if exclude_uci and key in exclude_uci:
            continue
        if key in seen:
            continue

        try:
            after_blunder = base.copy()
            after_blunder.push(bm)
        except Exception:
            continue

        # Punição imediata: mate em 1
        mate = _mate_in_one(after_blunder)
        if mate is not None:
            # Mate em 1 é, por definição, "forçado" — mostramos a linha completa.
            line_moves = [bm, mate]
            txt_forced = pv_em_san_numbered(base, line_moves, max_plies=len(line_moves)) if PV_WITH_NUMBERS else pv_em_san(base, line_moves, len(line_moves))
            if forced_best is None or forced_best[0] > 1:
                forced_best = (1, txt_forced)
            seen.add(key)
            continue

        # Sem cheque/captura disponível pro punidor? geralmente não é "armadilha" imediata.
        if not _has_check_or_capture(after_blunder):
            continue

        # Confirma com engine leve: existe punição tática forte?
        try:
            info = analyse_cached_single(after_blunder, time_s=engine_time_s)
            score = info.get("score")
            pv_punish = info.get("pv", [])
            if score is None or not pv_punish:
                continue

            punisher = after_blunder.turn
            cp_punisher = score_to_cp(score, punisher)
            mate_punisher = score.pov(punisher).mate() if score.pov(punisher).is_mate() else None

            is_forced_mate = mate_punisher is not None and mate_punisher > 0

            if not is_forced_mate and cp_punisher < min_cp_advantage:
                continue

            if is_forced_mate:
                # Linha "completa": tenta estender até o mate aparecer.
                seed = []
                temp = after_blunder.copy()
                for mm in pv_punish:
                    if mm not in temp.legal_moves:
                        break
                    seed.append(mm)
                    temp.push(mm)
                    if temp.is_checkmate():
                        break

                extended = _extend_pv_until_mate(after_blunder, seed, max_plies=32, time_s=max(0.05, engine_time_s))
                line_moves = [bm] + extended

                txt_forced = pv_em_san_numbered(base, line_moves, max_plies=len(line_moves)) if PV_WITH_NUMBERS else pv_em_san(base, line_moves, len(line_moves))
                if forced_best is None or mate_punisher < forced_best[0]:
                    forced_best = (int(mate_punisher), txt_forced)

                # Não adiciona versão curta: a completa já é o que importa.
                seen.add(key)
                continue

            follow = []
            temp = after_blunder.copy()
            for mm in pv_punish[: max(1, pv_plies - 1)]:
                if mm not in temp.legal_moves:
                    break
                follow.append(mm)
                temp.push(mm)

            line_moves = [bm] + follow
            txt = pv_em_san_numbered_autograded(base, line_moves, max_plies=len(line_moves)) if PV_WITH_NUMBERS else pv_em_san(base, line_moves, len(line_moves))
            if len(results) < max_lines:
                results.append(txt)
                seen.add(key)
        except Exception:
            continue

    # Garante que a melhor linha de mate forçado apareça, mesmo que passe do limite.
    if forced_best is not None:
        forced_txt = forced_best[1]
        if forced_txt not in results:
            return [forced_txt] + results
        # Se já existe versão curta em results, ainda prefixamos a completa.
        return [forced_txt] + [r for r in results if r != forced_txt]

    return results


def danger_heuristic(board_start: chess.Board, pv, *, hero_color: chess.Color, max_plies: int = 6) -> str:
    """Retorna uma combinação curta (mini-PV) quando houver perigo real para o herói.

    Objetivo: substituir texto genérico por uma sequência concreta de lances do adversário
    (cheque/mate/ganho tático de material) já presente na PV, sem chamadas extras de engine.
    """

    if not pv:
        return ""

    temp = board_start.copy()

    # Procura o primeiro "evento de perigo" na PV.
    for idx, m in enumerate((pv or [])[:max_plies]):
        if m not in temp.legal_moves:
            break

        mover = temp.turn
        captured = temp.piece_at(m.to_square)

        is_danger = False

        # Cheque/mate aplicado pelo adversário
        if mover != hero_color:
            try:
                if temp.gives_check(m):
                    is_danger = True
            except Exception:
                pass

        # Captura tática de material relevante do herói (>= peça menor)
        if captured is not None and captured.color == hero_color:
            if _piece_value(captured.piece_type) >= 3:
                is_danger = True

        # Mate após o lance (mesmo se gives_check falhar)
        try:
            after = temp.copy()
            after.push(m)
            if after.is_checkmate():
                is_danger = True
        except Exception:
            pass

        if is_danger:
            # Mostra a combinação até o perigo + defesa + continuação (curto, focado).
            snippet_plies = min(max_plies, idx + 3)
            if PV_WITH_NUMBERS:
                return pv_em_san_numbered(board_start, pv, snippet_plies)
            return pv_em_san(board_start, pv, snippet_plies)

        temp.push(m)

    # Compat: mantém assinatura antiga, mas retorna só a 1ª variante encontrada
    lines = danger_variants(board_start, pv, victim_color=hero_color, start_after_plies=1, max_lines=1)
    return lines[0] if lines else ""


def alerta_tatica_pos_lance(board_after: chess.Board, info_after: Dict[str, Any], *, pov: chess.Color, contexto: str = "") -> None:
    """Emite um alerta barato de tática/mate após um lance, usando APENAS a análise já calculada.

    Isso cobre a intenção de `evitar_brilhante_engine` sem custo extra de engine.
    """
    try:
        score = info_after.get("score")
        pv = info_after.get("pv", [])
        if score is None or not pv:
            return

        pov_score = score.pov(pov)
        cp = score_to_cp(score, pov)
        mate = pov_score.mate() if pov_score.is_mate() else None

        pv_txt = pv_em_san_numbered(board_after, pv, min(PV_PLIES_MAIN, 8)) if PV_WITH_NUMBERS else pv_em_san(board_after, pv, min(PV_PLIES_MAIN, 8))

        prefix = (contexto.strip() + ": ") if contexto.strip() else ""

        if mate is not None and mate < 0:
            print(f"  {_label('Perigo:')} {prefix}mate contra você em {-mate}: {pv_txt}")
            return

        if cp <= -250:
            print(f"  {_label('Perigo:')} {prefix}tática forte ({fmt_cp(cp)}): {pv_txt}")
            return

        if cp <= -120:
            print(f"  {_label('Cuidado:')} {prefix}sequência forte ({fmt_cp(cp)}): {pv_txt}")
    except Exception:
        return


def verificar_brilhante_inevitavel(board_before: chess.Board, move_obj: chess.Move, pov: chess.Color) -> bool:
    """Retorna True se o lance mantém grande vantagem contra as melhores defesas."""
    board_after = board_before.copy()
    board_after.push(move_obj)

    # Defesas do adversário (melhores tentativas)
    defesas = get_engine().analyse(board_after, chess.engine.Limit(time=max(0.25, ANALYSIS_TIME_MOVE)), multipv=3)
    piores_cp = None

    for linha in defesas:
        pv = linha.get("pv", [])
        if not pv:
            continue
        defesa = pv[0]
        if defesa not in board_after.legal_moves:
            continue

        after_def = board_after.copy()
        after_def.push(defesa)
        # Melhor resposta do seu lado (pra medir se ainda fica bom)
        resp = get_engine().analyse(after_def, chess.engine.Limit(time=max(0.25, ANALYSIS_TIME_MOVE)))
        cp = score_to_cp(resp["score"], pov)

        piores_cp = cp if piores_cp is None else min(piores_cp, cp)

    if piores_cp is None:
        return False

    # Se mesmo na pior defesa você segue ganhando bem, o sacrifício é "sólido".
    return piores_cp >= 180

def mostrar_sugestoes(board):

    pov = board.turn
    info = analyse_sorted(board, time_s=ANALYSIS_TIME_SUGGEST, multipv=SUGGEST_MULTIPV, pov_color=pov)
    melhor_cp = score_to_cp(info[0]["score"], pov)
    melhor_wp = win_prob_from_cp(melhor_cp)

    melhores = []

    for linha in info:
        pv = linha.get("pv", [])
        score = linha["score"]

        if not pv:
            continue

        move = pv[0]

        try:
            san = board.san(move)
        except:
            san = str(move)

        score_linha_cp = score_to_cp(score, pov)
        perda_cp = melhor_cp - score_linha_cp
        wp_loss = melhor_wp - win_prob_from_cp(score_linha_cp)
        qualidade = classificar_lance(perda_cp, wp_loss)

        entrada = {
            "san": san,
            "pv": pv,
            "qualidade": qualidade,
            "score": score,
            "perda_cp": int(perda_cp),
            "wp_loss": float(wp_loss),
            "uci": move.uci(),
        }

        if qualidade in ["!", "★"]:
            melhores.append(entrada)

    titulo("Respostas interessantes do adversário")
    seen_dangers: set[str] = set()
    for _, m in enumerate(melhores[:SHOW_OPPONENT_BEST]):
        if PV_WITH_NUMBERS:
            pv_txt = pv_em_san_numbered_autograded(board, m["pv"], max_plies=PV_PLIES_MAIN, first_grade_override=m["qualidade"])
        else:
            pv_txt = pv_em_san(board, m["pv"], PV_PLIES_MAIN)
        q = _grade_for_display(m.get("qualidade", ""))
        qtxt = f" {q}" if q else ""
        print(f"- {m['san']}{qtxt} | {pv_txt}")

        # Mantém somente alerta de perigo (sem ideias/planos)
        pv = m.get("pv", [])
        # Aqui o "perigo" é para o adversário (quem está escolhendo a resposta agora):
        # após ele jogar e você responder (PV[0], PV[1]), se ele se descuidar na volta,
        # quais punições concretas existem.
        victim = board.turn
        dangers_g = danger_variants_grouped(
            board,
            pv,
            victim_color=victim,
            start_after_plies=2,
            max_groups=MAX_DANGERS,
            max_subs=MAX_DANGER_SUBS,
            max_blunders=18,
            engine_time_s=0.08,
            min_cp_advantage=240,
            pv_plies=PV_PLIES_MAIN,
        )
        _print_dangers_grouped(
            dangers_g,
            label="Perigo",
            global_seen=(seen_dangers if DEDUP_DANGERS_ACROSS_LINES else None),
        )

    # Preview: se o adversário NÃO jogar nenhum dos melhores lances recomendados acima,
    # quais punições concretas você tem (máx 3, sem sub-variantes).
    exclude = {m.get("uci") for m in melhores[:SHOW_OPPONENT_BEST] if m.get("uci")}
    preview_g = danger_variants_grouped(
        board,
        [],
        victim_color=board.turn,
        start_after_plies=0,
        max_groups=OPPONENT_MISTAKES_PREVIEW_MAX,
        max_subs=0,
        max_blunders=22,
        engine_time_s=0.10,
        min_cp_advantage=180,
        pv_plies=PV_PLIES_MAIN,
        exclude_uci=exclude,
    )
    if preview_g:
        titulo("Se o adversário errar (como punir)")
        for item in preview_g[:OPPONENT_MISTAKES_PREVIEW_MAX]:
            print(f"- {item['main']}")

def detectar_brilhante(board_antes, move, melhor_score):
    piece = board_antes.piece_at(move.from_square)
    captured = board_antes.piece_at(move.to_square)

    # sacrifício: perde material
    if captured is None and piece.piece_type != chess.PAWN:
        return True

    return False

def analisar_seu_lance(board_antes, move_jogado):

    # Linhas principais para o lado a jogar.
    info_cont = analyse_sorted(board, time_s=ANALYSIS_TIME_SUGGEST, multipv=SHOW_MAIN_LINE, pov_color=board.turn)

    # Nota por variante: compara cada linha com a melhor, do POV de quem vai jogar agora.
    best_cp_turn = score_to_cp(info_cont[0]["score"], board.turn)
    best_wp_turn = win_prob_from_cp(best_cp_turn)

    # POV para exibir avaliação do "seu" lado (quem acabou de jogar)
    pov_me = not board.turn
    if STUDY_MODE:
        titulo(f"Linhas principais ({SHOW_MAIN_LINE})")

    for i, linha in enumerate(info_cont[:max(1, SHOW_MAIN_LINE)]):
        pv = linha.get("pv", [])
        if not pv:
            continue

        cp = score_to_cp(linha["score"], pov_me)
        label = "Sólida" if i == 0 else f"Opção {i+1}"

        line_cp_turn = score_to_cp(linha["score"], board.turn)
        perda_cp = max(0, best_cp_turn - line_cp_turn)
        wp_loss = max(0.0, best_wp_turn - win_prob_from_cp(line_cp_turn))
        qualidade = classificar_lance(perda_cp, wp_loss)

        if PV_WITH_NUMBERS:
            pv_txt = pv_em_san_numbered_autograded(board, pv, max_plies=PV_PLIES_MAIN, first_grade_override=qualidade)
        else:
            pv_txt = pv_em_san(board, pv, PV_PLIES_MAIN)
        print(f"- {label} ({fmt_cp(cp)}): {pv_txt}")
        # Perigo para você: após o 1º lance do adversário na PV, se você se descuidar,
        # qual punição concreta existe.
        dangers_g = danger_variants_grouped(
            board,
            pv,
            victim_color=not board.turn,
            start_after_plies=1,
            max_groups=MAX_DANGERS,
            max_subs=MAX_DANGER_SUBS,
            max_blunders=18,
            engine_time_s=0.08,
            min_cp_advantage=240,
            pv_plies=PV_PLIES_MAIN,
        )
        if i == 0:
            seen_dangers_main: set[str] = set()
        _print_dangers_grouped(
            dangers_g,
            label="Perigo",
            global_seen=(seen_dangers_main if DEDUP_DANGERS_ACROSS_LINES else None),
        )

    qualidade_melhor = ["!", "★"]

    pov = board_antes.turn
    info = analyse_sorted(board_antes, time_s=max(ANALYSIS_TIME_SUGGEST, 1.5), multipv=5, pov_color=pov)

    melhores = []

    # variantes_posicao = None

    melhor_cp = score_to_cp(info[0]["score"], pov)
    melhor_wp = win_prob_from_cp(melhor_cp)

    for linha in info:
        pv = linha.get("pv", [])
        score = linha["score"]

        if not pv:
            continue

        move = pv[0]

        try:
            san = board_antes.san(move)
        except:
            san = str(move)

        if san == move_jogado:
            continue

        line_cp = score_to_cp(score, pov)
        perda_cp = melhor_cp - line_cp
        wp_loss = melhor_wp - win_prob_from_cp(line_cp)
        qualidade = classificar_lance(perda_cp, wp_loss)

        entrada = {
            "san": san,
            "pv": pv,
            "qualidade": qualidade,
            "score": score
        }

        if qualidade in qualidade_melhor:
            melhores.append(entrada)

    # =========================
    # 🔝 TOP 3 QUE VOCÊ DEVERIA TER JOGADO
    # =========================
    if len(melhores) > 0:
        titulo("Outras opções")
        seen_dangers_alt: set[str] = set()
        for idx, m in enumerate(melhores[:SHOW_BETTER_MOVES]):
            cp = score_to_cp(m["score"], board_antes.turn)
            if PV_WITH_NUMBERS:
                pv_txt = pv_em_san_numbered_autograded(board_antes, m["pv"], max_plies=PV_PLIES_MAIN, first_grade_override=m["qualidade"])
            else:
                pv_txt = pv_em_san(board_antes, m["pv"], PV_PLIES_MAIN)
            q = _grade_for_display(m.get("qualidade", ""))
            qtxt = f" {q}" if q else ""
            print(f"- {m['san']}{qtxt} ({fmt_cp(cp)}): {pv_txt}")
            pv = m.get("pv", [])
            # Perigo para você nas opções alternativas: após a resposta do adversário (PV[1])
            dangers_g = danger_variants_grouped(
                board_antes,
                pv,
                victim_color=board_antes.turn,
                start_after_plies=1,
                max_groups=MAX_DANGERS,
                max_subs=MAX_DANGER_SUBS,
                max_blunders=18,
                engine_time_s=0.08,
                min_cp_advantage=240,
                pv_plies=PV_PLIES_MAIN,
            )
            _print_dangers_grouped(
                dangers_g,
                label="Perigo",
                global_seen=(seen_dangers_alt if DEDUP_DANGERS_ACROSS_LINES else None),
            )

    # (Brilhante aqui era ruído: o importante é classificar o lance jogado, não as alternativas.)

def evitar_brilhante_engine(board, adversario=False, possivel_lance="", round=1):
    """Alerta de armadilhas: procura a melhor sequência do adversário e avisa se for crítica."""

    # Aqui, o lado que acabou de jogar é o oposto de board.turn.
    pov = not board.turn

    info = get_engine().analyse(board, chess.engine.Limit(time=max(0.25, ANALYSIS_TIME_MOVE)), multipv=3)
    info.sort(key=lambda x: score_to_cp(x["score"], pov), reverse=True)

    if not info:
        return

    melhor = info[0]
    pv = melhor.get("pv", [])
    if not pv:
        return

    pov_score = melhor["score"].pov(pov)
    cp = score_to_cp(melhor["score"], pov)

    # Critérios de alerta: mate contra você, ou avaliação muito ruim.
    mate = pov_score.mate() if pov_score.is_mate() else None

    if mate is not None and mate < 0:
        titulo(f"ALERTA: armadilha após {possivel_lance}")
        pv_txt = pv_em_san_numbered(board, pv, PV_PLIES_MAIN) if PV_WITH_NUMBERS else pv_em_san(board, pv, PV_PLIES_MAIN)
        print(f"- Mate contra você em {-mate}: {pv_txt}")
        return

    if cp <= -250:
        titulo(f"ALERTA: tática após {possivel_lance}")
        pv_txt = pv_em_san_numbered(board, pv, PV_PLIES_MAIN) if PV_WITH_NUMBERS else pv_em_san(board, pv, PV_PLIES_MAIN)
        print(f"- Piora forte ({fmt_cp(cp)}): {pv_txt}")
        return

    if cp <= -120:
        # if not STUDY_MODE:
        titulo(f"CUIDADO após {possivel_lance}")
        pv_txt = pv_em_san_numbered(board, pv, PV_PLIES_MAIN) if PV_WITH_NUMBERS else pv_em_san(board, pv, PV_PLIES_MAIN)
        print(f"- Sequência forte ({fmt_cp(cp)}): {pv_txt}")

def normalizar_lance(move):
    move = move.strip()

    if not move:
        return move

    # se começa com letra de peça (minúscula), só deixa maiúscula
    if move[0] in "rnbqk":
        move = move[0].upper() + move[1:]

    return move


def escolher_cor_inicial() -> chess.Color:
    """Pergunta ao usuário se joga de brancas ou pretas (default: brancas)."""
    try:
        raw = input("Jogar de brancas ou pretas? [b/p] (default b): ").strip().lower()
    except Exception:
        return chess.WHITE

    if not raw or raw.startswith("b") or raw.startswith("w"):
        return chess.WHITE
    if raw.startswith("p") or raw.startswith("n"):
        # p = pretas; n = negras
        return chess.BLACK
    return chess.WHITE


def jogar_lance_engine() -> None:
    """Faz o lance da engine (lado que estiver com a vez) e registra/avalia."""
    global moves_san, avaliacoes

    if board.is_game_over():
        return

    board_antes_engine = board.copy()
    result = get_engine().play(board, chess.engine.Limit(depth=15))
    engine_move = result.move
    san_engine = board.san(engine_move)

    board.push(engine_move)

    # Se acabou em mate, não tenta "classificar" por cp_loss.
    if board.is_checkmate() or san_engine.endswith("#"):
        qualidade_eng = "#"
        print(f"\nEngine: {san_engine} {qualidade_eng} ({desc_qualidade(qualidade_eng)})")

        moves_san.append(san_engine)
        avaliacoes.append("")  # o san já vem com '#'
        imprimir_partida(moves_san, avaliacoes)
        return

    # Avaliação do lance da engine (comparação correta na posição dela)
    pov_e = board_antes_engine.turn
    info_best_e = analyse_sorted(board_antes_engine, time_s=ANALYSIS_TIME_MOVE, multipv=5, pov_color=pov_e)
    best_cp_e = score_to_cp(info_best_e[0]["score"], pov_e)
    best_wp_e = win_prob_from_cp(best_cp_e)

    played_info_e = get_engine().analyse(board, chess.engine.Limit(time=ANALYSIS_TIME_MOVE))
    played_cp_e = score_to_cp(played_info_e["score"], pov_e)
    cp_loss_e = max(0, best_cp_e - played_cp_e)
    wp_loss_e = max(0.0, best_wp_e - win_prob_from_cp(played_cp_e))

    qualidade_eng = classificar_lance(cp_loss_e, wp_loss_e)
    qualidade_eng = ajustar_nota_abertura(board_antes_engine, qualidade_eng, cp_loss_e)

    q = _grade_for_display(qualidade_eng)
    qtxt = f" {q}" if q else ""
    print(f"\nEngine: {san_engine}{qtxt} ({desc_qualidade(qualidade_eng)})")

    moves_san.append(san_engine)
    avaliacoes.append(qualidade_eng)
    imprimir_partida(moves_san, avaliacoes)

def main() -> None:
    # Inicializa a engine só quando executa o script.
    get_engine()

    global HERO_COLOR
    HERO_COLOR = escolher_cor_inicial()

    _hint_code_legend()

    # Se o humano escolheu pretas, a engine (brancas) abre a partida.
    if board.turn != HERO_COLOR and not board.is_game_over():
        jogar_lance_engine()

    while not board.is_game_over():

        # Se por algum motivo não for a vez do humano, deixa a engine jogar.
        if board.turn != HERO_COLOR:
            jogar_lance_engine()
            continue

        board_antes = board.copy()

        # Código sempre antes do seu lance (pós jogada da engine). No início, só aparece
        # se a engine abriu (humano de pretas).
        if len(board.move_stack) > 0:
            _print_hint_code(board)

        try:
            lado = "Brancas" if board.turn == chess.WHITE else "Pretas"
            move = input(f"Seu lance ({lado}): ")
        except KeyboardInterrupt:
            print("\nEncerrado.")
            break
        except EOFError:
            print("\nEncerrado.")
            break
        move = normalizar_lance(move)

        try:
            move_obj = board.parse_san(move)
        except Exception:
            print("\nLances legais:")
            print([board.san(m) for m in board.legal_moves])
            print("❌ Lance inválido")
            continue

        if move_obj not in board.legal_moves:
            print("❌ Lance ilegal nessa posição")
            continue

        san = board.san(move_obj)
        board.push(move_obj)
        moves_san.append(san)

        # =========================
        # Avaliação do seu lance (mesmo POV antes/depois)
        # =========================
        pov = board_antes.turn
        info_best = analyse_sorted(board_antes, time_s=ANALYSIS_TIME_MOVE, multipv=5, pov_color=pov)
        best_cp = score_to_cp(info_best[0]["score"], pov)
        best_wp = win_prob_from_cp(best_cp)

        played_info = get_engine().analyse(board, chess.engine.Limit(time=ANALYSIS_TIME_MOVE))
        played_cp = score_to_cp(played_info["score"], pov)
        cp_loss = max(0, best_cp - played_cp)
        wp_loss = max(0.0, best_wp - win_prob_from_cp(played_cp))

        avaliacao = classificar_lance(cp_loss, wp_loss)

        # Refina apenas em casos de fronteira (reduz "vai e volta" de ?! / ?).
        avaliacao, best_cp, best_wp, played_cp = _refinar_classificacao_se_fronteira(
            board_antes,
            board,
            pov=pov,
            best_cp=best_cp,
            best_wp=best_wp,
            played_cp=played_cp,
            grade=avaliacao,
            cp_loss=cp_loss,
            wp_loss=wp_loss,
        )

        # Regras duras estilo chess.com (independente de cp_loss em posição já perdida)
        try:
            s_best = info_best[0]["score"].pov(pov)
            s_played = played_info["score"].pov(pov)

            # Permitiu mate contra você rápido? -> gafe
            if s_played.is_mate():
                m = s_played.mate()
                if m is not None and m < 0 and (-m) <= 6:
                    avaliacao = "??"

            # Perdeu mate forçado que você tinha? -> geralmente gafe
            if s_best.is_mate():
                bm = s_best.mate()
                if bm is not None and bm > 0 and bm <= 4:
                    pm = s_played.mate() if s_played.is_mate() else None
                    if pm is None or pm <= 0 or pm > bm:
                        avaliacao = "??"
        except Exception:
            pass

        avaliacao = ajustar_nota_abertura(board_antes, avaliacao, cp_loss)

        # Heurística mínima de treino (ex.: f3 cedo = erro).
        avaliacao = _heuristica_fraqueza_rei_minima(board_antes, move_obj, avaliacao)
        if detectar_brilhante_por_sacrificio(board_antes, move_obj, played_info, played_cp, avaliacao):
            # Só é "!!" se for sólido mesmo contra boas defesas.
            if verificar_brilhante_inevitavel(board_antes, move_obj, pov):
                avaliacao = "!!"
        avaliacoes.append(avaliacao)

        print(f"\n{san}: {avaliacao} ({desc_qualidade(avaliacao)})")
        # Alerta de tática/mate com base na melhor continuação do adversário (sem engine extra).
        alerta_tatica_pos_lance(board, played_info, pov=pov, contexto=san)

        analisar_seu_lance(board_antes, san)
        # evitar_brilhante_engine(board)

        # sugestões (em modo compacto, esta seção já vira ruído e é suprimida)
        mostrar_sugestoes(board)

        # engine joga
        jogar_lance_engine()

    _shutdown_engine()


if __name__ == "__main__":
    main()