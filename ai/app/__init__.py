"""SpeakUp AI service."""

import sys
from pathlib import Path

_GEN_ROOT = str(Path(__file__).resolve().parents[1] / "gen")
if _GEN_ROOT not in sys.path:
    sys.path.insert(0, _GEN_ROOT)
