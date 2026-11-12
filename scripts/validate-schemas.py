#!/usr/bin/env python3
"""Validate every JSON Schema in schemas/ and structurally check api/openapi.yaml.

Requires: pip install jsonschema pyyaml
"""
import glob
import json
import sys

import yaml
from jsonschema import Draft202012Validator

failed = False
for path in sorted(glob.glob("schemas/*.json")):
    try:
        Draft202012Validator.check_schema(json.load(open(path)))
        print(f"ok   {path}")
    except Exception as e:  # noqa: BLE001
        print(f"FAIL {path}: {e}")
        failed = True

try:
    doc = yaml.safe_load(open("api/openapi.yaml"))
    assert str(doc["openapi"]).startswith("3.1"), "openapi must be 3.1.x"
    assert doc["paths"], "no paths"
    for p, ops in doc["paths"].items():
        for m, op in ops.items():
            assert op.get("responses"), f"{m.upper()} {p}: no responses"
    print("ok   api/openapi.yaml")
except Exception as e:  # noqa: BLE001
    print(f"FAIL api/openapi.yaml: {e}")
    failed = True

sys.exit(1 if failed else 0)
