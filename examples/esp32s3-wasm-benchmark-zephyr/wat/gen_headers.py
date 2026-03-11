#!/usr/bin/env python3
import argparse
import os
import subprocess
import sys
import tempfile

def wat_to_c_array(wat_path):
    with tempfile.NamedTemporaryFile(suffix=".wasm", delete=False) as f:
        wasm_path = f.name
    try:
        subprocess.run(["wat2wasm", wat_path, "-o", wasm_path], check=True)
        with open(wasm_path, "rb") as f:
            data = f.read()
    finally:
        os.unlink(wasm_path)

    name = os.path.splitext(os.path.basename(wat_path))[0]
    rows = [data[i:i+16] for i in range(0, len(data), 16)]
    hex_rows = ",\n    ".join(
        ", ".join(f"0x{b:02x}" for b in row) for row in rows
    )
    return (
        f"static const uint8_t {name}[] = {{\n"
        f"    {hex_rows},\n"
        f"}};\n"
        f"static const uint32_t {name}_len = sizeof({name});\n"
    )

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("wats", nargs="+")
    args = parser.parse_args()

    with open(args.output, "w") as out:
        out.write("#pragma once\n#include <stdint.h>\n\n")
        for wat in args.wats:
            out.write(wat_to_c_array(wat))
            out.write("\n")

    print(f"Written {args.output}")

if __name__ == "__main__":
    main()
