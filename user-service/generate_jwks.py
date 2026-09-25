import json
import os
import argparse
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives import serialization


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


def main():
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

    # The Go service computes the public keys and kids programmatically,
    # so we only need to provide the raw private keys.
    payload = {"keys": [generate_rsa_private_key_pem() for _ in range(args.count)]}

    # Open file with restricted 0600 permissions (read/write for owner only)
    flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC
    try:
        fd = os.open(args.out, flags, 0o600)
        with open(fd, "w", encoding="utf-8") as f:
            json.dump(payload, f, indent=2)
        print(
            f"Success: Wrote {args.count} RSA private keys to {args.out} with 0600 permissions."
        )
    except Exception as e:
        print(f"Error writing keys: {e}")


if __name__ == "__main__":
    main()
