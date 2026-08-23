import argparse
import asyncio

import edge_tts

# Per-style tuning keeps 50 catalog entries audibly distinct on the Python
# fallback path (Go WebSocket is preferred when the network allows it).
STYLE_TUNING = {
    "chat": ("+0%", "+0Hz"),
    "cheerful": ("+8%", "+4Hz"),
    "affectionate": ("+2%", "+2Hz"),
    "gentle": ("-4%", "-2Hz"),
    "lyrical": ("+3%", "+1Hz"),
    "calm": ("-6%", "-3Hz"),
    "empathetic": ("-2%", "+1Hz"),
    "sad": ("-8%", "-4Hz"),
    "serious": ("-3%", "-1Hz"),
    "newscast": ("+4%", "+0Hz"),
    "customerservice": ("+1%", "+2Hz"),
    "assistant": ("+0%", "+3Hz"),
    "poetry-reading": ("-5%", "+2Hz"),
    "sports-commentary": ("+10%", "+5Hz"),
    "narration-relaxed": ("-4%", "-1Hz"),
    "narration-professional": ("+2%", "-2Hz"),
}


def volume_token(raw: str) -> str:
    vol = int(raw)
    return f"{vol - 100:+d}%"


async def synth(args: argparse.Namespace) -> None:
    rate = args.rate
    pitch = "+0Hz"
    if args.style:
        rate, pitch = STYLE_TUNING.get(args.style, (args.rate, "+0Hz"))
    comm = edge_tts.Communicate(
        args.text,
        args.voice,
        rate=rate,
        volume=volume_token(args.volume),
        pitch=pitch,
    )
    await comm.save(args.out)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--voice", required=True)
    parser.add_argument("--text", required=True)
    parser.add_argument("--style", default="")
    parser.add_argument("--rate", default="+0%")
    parser.add_argument("--volume", default="100")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    asyncio.run(synth(args))


if __name__ == "__main__":
    main()
