#!/usr/bin/env python3
"""Convert the official reference interpreter's binary script to WABT-like JSON.

The Release 3 interpreter accepts text forms that WABT 1.0.41 does not. Running
it in dry mode with a .bin.wast output preserves script commands while replacing
every embedded module with exact binary bytes. This converter keeps those bytes
and emits the command shape consumed by wago's native spectest harness.
"""

import argparse
import json
import math
import struct
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Token:
    kind: str
    value: str
    line: int


@dataclass
class Form:
    items: list
    line: int


def tokenize(text):
    tokens = []
    i = 0
    line = 1
    n = len(text)
    while i < n:
        c = text[i]
        if c.isspace():
            if c == "\n":
                line += 1
            i += 1
            continue
        if c == ";" and i + 1 < n and text[i + 1] == ";":
            i += 2
            while i < n and text[i] != "\n":
                i += 1
            continue
        if c in "()":
            tokens.append(Token(c, c, line))
            i += 1
            continue
        if c == '"':
            start_line = line
            i += 1
            out = []
            while i < n and text[i] != '"':
                c = text[i]
                if c == "\n":
                    raise ValueError(f"line {line}: newline in string")
                if c != "\\":
                    out.append(c)
                    i += 1
                    continue
                i += 1
                if i >= n:
                    raise ValueError(f"line {line}: unterminated escape")
                if i + 1 < n and all(ch in "0123456789abcdefABCDEF" for ch in text[i:i + 2]):
                    out.append(chr(int(text[i:i + 2], 16)))
                    i += 2
                    continue
                esc = text[i]
                i += 1
                escapes = {"n": "\n", "t": "\t", "r": "\r", "\\": "\\", '"': '"', "'": "'"}
                if esc in escapes:
                    out.append(escapes[esc])
                    continue
                if esc == "u" and i < n and text[i] == "{":
                    end = text.find("}", i + 1)
                    if end < 0:
                        raise ValueError(f"line {line}: unterminated unicode escape")
                    out.append(chr(int(text[i + 1:end], 16)))
                    i = end + 1
                    continue
                raise ValueError(f"line {line}: unsupported escape \\{esc}")
            if i >= n:
                raise ValueError(f"line {start_line}: unterminated string")
            i += 1
            tokens.append(Token("string", "".join(out), start_line))
            continue
        start = i
        while i < n and not text[i].isspace() and text[i] not in "()":
            i += 1
        tokens.append(Token("atom", text[start:i], line))
    return tokens


def parse(text):
    tokens = tokenize(text)
    pos = 0

    def one():
        nonlocal pos
        if pos >= len(tokens):
            raise ValueError("unexpected end of binary script")
        tok = tokens[pos]
        pos += 1
        if tok.kind != "(":
            if tok.kind == ")":
                raise ValueError(f"line {tok.line}: unexpected )")
            return tok
        items = []
        while True:
            if pos >= len(tokens):
                raise ValueError(f"line {tok.line}: unclosed form")
            if tokens[pos].kind == ")":
                pos += 1
                return Form(items, tok.line)
            items.append(one())

    forms = []
    while pos < len(tokens):
        forms.append(one())
    return forms


def atom(value):
    if not isinstance(value, Token) or value.kind != "atom":
        raise ValueError("expected atom")
    return value.value


def string(value):
    if not isinstance(value, Token) or value.kind != "string":
        raise ValueError("expected string")
    return value.value


def head(form):
    if not isinstance(form, Form) or not form.items:
        raise ValueError(f"line {getattr(form, 'line', '?')}: expected non-empty form")
    return atom(form.items[0])


def unsigned_int(text, bits):
    value = int(text, 0)
    return str(value & ((1 << bits) - 1))


def float_bits(text, bits):
    sign = 0
    raw = text
    if raw.startswith("-"):
        sign = 1
        raw = raw[1:]
    elif raw.startswith("+"):
        raw = raw[1:]
    if raw.startswith("nan:0x"):
        payload = int(raw[6:], 16)
        if bits == 32:
            return str((sign << 31) | 0x7F800000 | (payload & 0x7FFFFF))
        return str((sign << 63) | 0x7FF0000000000000 | (payload & 0xFFFFFFFFFFFFF))
    if raw in {"nan", "nan:canonical", "nan:arithmetic"}:
        return raw if raw != "nan" else "nan:canonical"
    signed = ("-" if sign else "") + raw
    if raw == "inf":
        value = -math.inf if sign else math.inf
    else:
        value = float.fromhex(signed) if raw.lower().startswith("0x") else float(signed)
    if bits == 32:
        return str(struct.unpack("<I", struct.pack("<f", value))[0])
    return str(struct.unpack("<Q", struct.pack("<d", value))[0])


def value(form):
    op = head(form)
    args = form.items[1:]
    if op in {"i32.const", "i64.const"}:
        bits = 32 if op == "i32.const" else 64
        return {"type": op[:3], "value": unsigned_int(atom(args[0]), bits)}
    if op in {"f32.const", "f64.const"}:
        bits = 32 if op == "f32.const" else 64
        return {"type": op[:3], "value": float_bits(atom(args[0]), bits)}
    if op == "v128.const":
        shape = atom(args[0])
        lane_bits = int(shape[1:shape.index("x")]) if shape.startswith("i") else int(shape[1:shape.index("x")])
        lanes = []
        for item in args[1:]:
            text = atom(item)
            lanes.append(float_bits(text, lane_bits) if shape.startswith("f") else unsigned_int(text, lane_bits))
        return {"type": "v128", "lane_type": shape.split("x", 1)[0], "value": lanes}
    if op == "ref.null":
        heap = atom(args[0]) if args else "func"
        typ = "externref" if heap in {"extern", "noextern"} else "funcref" if heap in {"func", "nofunc"} else "ref"
        return {"type": typ, "value": "null"}
    if op == "ref.func":
        return {"type": "funcref"}
    if op == "ref.extern":
        return {"type": "externref", "value": atom(args[0]) if args else "0"}
    if op == "ref.host":
        return {"type": "ref", "value": atom(args[0]) if args else "0"}
    if op.startswith("ref."):
        return {"type": "ref", "heap_type": op[4:]}
    raise ValueError(f"line {form.line}: unsupported script value {op}")


def action(form):
    op = head(form)
    items = form.items[1:]
    out = {"type": op}
    if items and isinstance(items[0], Token) and items[0].kind == "atom" and items[0].value.startswith("$"):
        out["module"] = atom(items.pop(0))
    if not items:
        raise ValueError(f"line {form.line}: {op} lacks field")
    out["field"] = string(items.pop(0))
    if op == "invoke":
        out["args"] = [value(item) for item in items]
    elif items:
        raise ValueError(f"line {form.line}: get has unexpected operands")
    return out


class Converter:
    def __init__(self, output):
        self.output = output
        self.commands = []
        self.module_index = 0

    def write_module(self, form):
        items = list(form.items[1:])
        if items and isinstance(items[0], Token) and atom(items[0]) == "definition":
            items.pop(0)
        name = ""
        if items and isinstance(items[0], Token) and items[0].kind == "atom" and items[0].value.startswith("$"):
            name = atom(items.pop(0))
        if not items or atom(items.pop(0)) != "binary":
            raise ValueError(f"line {form.line}: expected binary module")
        data = b"".join(string(item).encode("latin1") for item in items)
        filename = f"{self.output.stem}.{self.module_index}.wasm"
        self.module_index += 1
        self.output.with_name(filename).write_bytes(data)
        return filename, name

    def module(self, form):
        items = form.items
        kind = atom(items[1]) if len(items) > 1 and isinstance(items[1], Token) and items[1].kind == "atom" else ""
        if kind == "instance":
            names = [atom(item) for item in items[2:]]
            if len(names) > 2:
                raise ValueError(f"line {form.line}: too many module instance names")
            cmd = {"type": "module_instance", "line": form.line}
            if names:
                cmd["name"] = names[0]
            if len(names) == 2:
                cmd["module"] = names[1]
            self.commands.append(cmd)
            return
        filename, name = self.write_module(form)
        cmd = {"type": "module_definition" if kind == "definition" else "module", "line": form.line, "filename": filename}
        if name:
            cmd["name"] = name
        self.commands.append(cmd)

    def assertion_module(self, form, typ):
        module = form.items[1]
        cmd = {"type": typ, "line": form.line, "module_type": "binary"}
        if len(module.items) > 1 and atom(module.items[1]) == "instance":
            names = [atom(item) for item in module.items[2:]]
            if len(names) > 2:
                raise ValueError(f"line {module.line}: too many asserted module instance names")
            if names:
                cmd["name"] = names[0]
            if len(names) == 2:
                cmd["module"] = names[1]
            cmd["module_type"] = "instance"
        else:
            filename, _ = self.write_module(module)
            cmd["filename"] = filename
        if len(form.items) > 2 and isinstance(form.items[2], Token) and form.items[2].kind == "string":
            cmd["text"] = string(form.items[2])
        self.commands.append(cmd)

    def assertion_action(self, form, typ):
        cmd = {"type": typ, "line": form.line, "action": action(form.items[1])}
        rest = form.items[2:]
        if typ == "assert_return":
            if len(rest) == 1 and isinstance(rest[0], Form) and head(rest[0]) == "either":
                cmd["either"] = [value(item) for item in rest[0].items[1:]]
            else:
                cmd["expected"] = [value(item) for item in rest]
        elif rest and isinstance(rest[0], Token) and rest[0].kind == "string":
            cmd["text"] = string(rest[0])
        self.commands.append(cmd)

    def convert(self, forms):
        for form in forms:
            op = head(form)
            if op == "module":
                self.module(form)
            elif op == "register":
                cmd = {"type": "register", "line": form.line, "as": string(form.items[1])}
                if len(form.items) > 2:
                    cmd["name"] = atom(form.items[2])
                self.commands.append(cmd)
            elif op in {"invoke", "get"}:
                self.commands.append({"type": "action", "line": form.line, "action": action(form)})
            elif op == "assert_return":
                self.assertion_action(form, op)
            elif op in {"assert_trap", "assert_exhaustion", "assert_exception"}:
                if isinstance(form.items[1], Form) and head(form.items[1]) == "module":
                    self.assertion_module(form, "assert_uninstantiable")
                else:
                    self.assertion_action(form, op)
            elif op in {"assert_malformed", "assert_invalid", "assert_unlinkable", "assert_uninstantiable"}:
                self.assertion_module(form, op)
            else:
                raise ValueError(f"line {form.line}: unsupported binary script command {op}")
        return {"source": "WebAssembly/spec interpreter 3.0.0", "commands": self.commands}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    forms = parse(args.input.read_text())
    document = Converter(args.output).convert(forms)
    args.output.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
