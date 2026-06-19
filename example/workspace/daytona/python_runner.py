#!/usr/bin/env python3
import argparse
import contextlib
import io
import json
import os
import runpy
import sys
import traceback


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--script", required=True)
    parser.add_argument("--csv", required=True)
    parser.add_argument("--result", required=True)
    parser.add_argument("--report", required=True)
    args = parser.parse_args()
    os.makedirs(os.path.dirname(args.result), exist_ok=True)
    os.makedirs(os.path.dirname(args.report), exist_ok=True)

    stdout = io.StringIO()
    stderr = io.StringIO()
    globals_for_script = {
        "CSV_PATH": args.csv,
        "RESULT_PATH": args.result,
        "REPORT_PATH": args.report,
    }
    status = "ok"
    error = ""
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        try:
            runpy.run_path(args.script, init_globals=globals_for_script, run_name="__main__")
        except Exception:
            status = "error"
            error = traceback.format_exc()

    result = None
    if os.path.exists(args.result):
        try:
            with open(args.result, encoding="utf-8") as source:
                result = json.load(source)
        except Exception:
            status = "error"
            if error:
                error += "\n"
            error += traceback.format_exc()
    elif status == "ok":
        status = "error"
        error = f"generated script did not write result JSON: {args.result}"

    payload = {
        "status": status,
        "error": error,
        "stdout": stdout.getvalue(),
        "stderr": stderr.getvalue(),
        "script_path": args.script,
        "result_path": args.result,
        "report_path": args.report,
        "data_source": {
            "csv_path": args.csv,
        },
        "result": result,
    }
    print(json.dumps(payload, ensure_ascii=False, sort_keys=True))
    return 0 if status == "ok" else 1


if __name__ == "__main__":
    sys.exit(main())
