import argparse
import json
import os
import sys

from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives import serialization

# AI Assistance Disclosure:
# Tool: Codex (GPT-5), date: 2026-09-28
# Scope: Enforced recorded key-file permissions and failure exit status, and added script attribution.
# Author review: ZI YANG - validated correctness


def generate_rsa_private_key_pem(key_size: int = 2048) -> str:
    """Generates an RSA private key and returns it as a PKCS#8 PEM string."""
    private_key = rsa.generate_private_key(
        public_exponent=65537,
        key_size=key_size,
    )

    pem_bytes = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )

    return pem_bytes.decode("utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Generate keys.json for CampusRun User Service"
    )
    parser.add_argument(
        "--count",
        type=int,
        default=2,
        help="Number of keys to generate (active + retired)",
    )
    parser.add_argument("--out", type=str, default="keys.json", help="Output file path")
    args = parser.parse_args()

    try:
        # The Go service computes the public keys and kids programmatically,
        # so this file contains only PEM private keys.
        payload = {"keys": [generate_rsa_private_key_pem() for _ in range(args.count)]}
        flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC
        fd = os.open(args.out, flags, 0o600)
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(payload, f, indent=2)
        print(
            f"Success: Wrote {args.count} RSA private keys to {args.out} with 0600 permissions."
        )
        return 0
    except Exception as error:
        print(f"Error writing keys: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
