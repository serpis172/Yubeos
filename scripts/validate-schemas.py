#!/usr/bin/env python3
"""Validate JSON Schemas, OpenAPI structure/references, and schema fixtures.

Requires: pip install jsonschema pyyaml   (installed by `make deps`)
Not a full OpenAPI linter (CI also runs one); it catches the structural mistakes that broke us before.
"""
import copy
import glob
import json
import re
import sys

import yaml
from jsonschema import Draft202012Validator

HTTP_METHODS = {"get", "put", "post", "delete", "options", "head", "patch", "trace"}
failures = []


def fail(msg):
    failures.append(msg)
    print(f"FAIL {msg}")


def ok(msg):
    print(f"ok   {msg}")


# 1. JSON Schemas are valid schemas
for path in sorted(glob.glob("schemas/*.json")):
    try:
        Draft202012Validator.check_schema(json.load(open(path)))
        ok(path)
    except Exception as e:  # noqa: BLE001
        fail(f"{path}: {e}")

# 2. OpenAPI structure + $ref resolution
try:
    doc = yaml.safe_load(open("api/openapi.yaml"))
    if not str(doc.get("openapi", "")).startswith("3.1"):
        raise ValueError("openapi must be 3.1.x")
    if not doc.get("paths"):
        raise ValueError("no paths")

    refs = []

    def collect(node):
        if isinstance(node, dict):
            for k, v in node.items():
                if k == "$ref" and isinstance(v, str):
                    refs.append(v)
                else:
                    collect(v)
        elif isinstance(node, list):
            for v in node:
                collect(v)

    collect(doc)
    for r in refs:
        if not r.startswith("#/"):
            raise ValueError(f"external $ref not allowed: {r}")
        cur = doc
        for part in r[2:].split("/"):
            if not isinstance(cur, dict) or part not in cur:
                raise ValueError(f"unresolved $ref {r}")
            cur = cur[part]

    seen_ids = set()
    for p, item in doc["paths"].items():
        for m, op in item.items():
            if m not in HTTP_METHODS:  # path-level keys: servers, parameters, summary, description
                continue
            if not op.get("responses"):
                raise ValueError(f"{m.upper()} {p}: no responses")
            if not any(str(c).startswith("2") for c in op["responses"]):
                raise ValueError(f"{m.upper()} {p}: no success response")
            if "operationId" in op:
                if op["operationId"] in seen_ids:
                    raise ValueError(f"duplicate operationId {op['operationId']}")
                seen_ids.add(op["operationId"])
            for t in op.get("tags", []):
                if t not in {x["name"] for x in doc.get("tags", [])}:
                    raise ValueError(f"{m.upper()} {p}: undeclared tag {t}")
        for tmpl in re.findall(r"\{(\w+)\}", p):
            for m, op in item.items():
                if m in HTTP_METHODS and not any(x.get("name") == tmpl and x.get("in") == "path" for x in op.get("parameters", [])):
                    raise ValueError(f"{m.upper()} {p}: path parameter {{{tmpl}}} not declared")
    ok(f"api/openapi.yaml ({len(doc['paths'])} paths, {len(refs)} refs resolved)")
except Exception as e:  # noqa: BLE001
    fail(f"api/openapi.yaml: {e}")

# 3. Add-on manifest fixtures: valid one passes, path-traversal variants are rejected
try:
    schema = json.load(open("schemas/addon.schema.json"))
    v = Draft202012Validator(schema)
    good = json.load(open("schemas/testdata/addon-valid.json"))
    errs = list(v.iter_errors(good))
    if errs:
        raise ValueError(f"valid manifest rejected: {errs[0].message}")
    mutations = {
        "storage.mount ..": lambda m: m["spec"]["storage"][0].update(mount="/data/../../etc"),
        "ui.path ..": lambda m: m["spec"]["ui"].update(path="/a/../b"),
        "compose with slash": lambda m: m["spec"].update(compose="../compose.yaml"),
        "device ..": lambda m: m["spec"]["permissions"].update(devices=["/dev/../etc/passwd"]),
        "unknown key": lambda m: m["spec"].update(surprise=True),
        "bad id": lambda m: m["metadata"].update(id="Bad ID"),
    }
    for name, mutate in mutations.items():
        bad = copy.deepcopy(good)
        mutate(bad)
        if not list(v.iter_errors(bad)):
            raise ValueError(f"manifest with '{name}' was ACCEPTED")
    ok(f"addon manifest fixtures ({len(mutations)} negative cases)")
except Exception as e:  # noqa: BLE001
    fail(f"addon fixtures: {e}")

sys.exit(1 if failures else 0)
