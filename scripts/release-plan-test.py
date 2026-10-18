#!/usr/bin/env python3
# Copyright 2026 ForgeSchema
# SPDX-License-Identifier: Apache-2.0
#
# Cases for scripts/release-plan.py. Run: python3 scripts/release-plan-test.py
import importlib.util
import os
import sys

spec = importlib.util.spec_from_file_location("plan", os.path.join(os.path.dirname(os.path.abspath(__file__)), "release-plan.py"))
plan = importlib.util.module_from_spec(spec)
spec.loader.exec_module(plan)

SHA, OTHER = "a" * 40, "b" * 40
TAGS = ["v0.1.0-alpha.1", "v0.2.0-alpha.1"]


def after(version, head=SHA, tags=TAGS, workflow="test"):
    return plan.Event("workflow_run", run_workflow=workflow, run_sha=SHA, version=version,
                      main_head=lambda: head, tags=lambda: tags)


def expect(name, ev, action=None, tag="", refuses=False):
    try:
        p = plan.decide(ev)
    except plan.Refusal as e:
        if refuses:
            return 0
        print(f"FAIL {name}: refused: {e}")
        return 1
    if refuses:
        print(f"FAIL {name}: expected a refusal, got {p.action}: {p.why}")
        return 1
    if p.action != action or (tag and p.tag != tag):
        print(f"FAIL {name}: got {p.action} {p.tag}: {p.why}")
        return 1
    return 0


failures = sum([
    expect("current alpha is out already", after("0.2.0-alpha.1"), "none"),
    expect("new full release", after("0.3.0"), "release", "v0.3.0"),
    expect("release ref is the run's commit", after("0.3.0"), "release"),
    expect("tag exists", after("0.3.0", tags=TAGS + ["v0.3.0"]), "none"),
    expect("lower than newest", after("0.3.0", tags=TAGS + ["v0.4.0"]), refuses=True),
    expect("an existing tag wins over the form check", after("0.2.0", tags=["v0.2.0"] + TAGS), "none"),
    expect("new alpha form refused", after("0.3.0-alpha.1"), refuses=True),
    expect("leading zero refused", after("0.03.0"), refuses=True),
    expect("missing version", after(None), refuses=True),
    expect("main moved on", after("0.3.0", head=OTHER), "none"),
    expect("other workflow", after("0.3.0", workflow="Release"), "none"),
    expect("release by hand", plan.Event("release", release_tag="v0.3.0"), "attach", "v0.3.0"),
    expect("run by hand", plan.Event("workflow_dispatch", input_tag="v0.3.0"), "attach", "v0.3.0"),
    expect("run by hand without tag", plan.Event("workflow_dispatch"), refuses=True),
    expect("push event", plan.Event("push"), "none"),
])
p = plan.decide(after("0.3.0"))
if p.ref != SHA or dict(p.outputs()) != {"tag": "v0.3.0", "ref": SHA, "new": "true"}:
    print(f"FAIL outputs: {p.outputs()}")
    failures += 1
print("release plan: all cases pass" if not failures else f"release plan: {failures} case(s) failed")
sys.exit(1 if failures else 0)
