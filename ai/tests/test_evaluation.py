from app.evaluation import evaluate


def test_evaluate_returns_five_dimensions() -> None:
    result = evaluate(
        {
            "transcript": [
                {"user_text": "I would like the tomato soup, please."},
                {"user_text": "Could I also have some water?"},
            ]
        }
    )

    assert result["overall_score"] > 0
    assert set(result["dimensions"]) == {
        "pronunciation",
        "grammar",
        "vocabulary",
        "fluency",
        "coherence",
    }


def test_evaluate_empty_transcript_is_zero() -> None:
    result = evaluate({"transcript": []})
    assert result["overall_score"] == 0
    assert result["issues"]
