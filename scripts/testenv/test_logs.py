"""logs.py 的单元测试：python3 -m unittest scripts/testenv/test_logs.py"""
import argparse
import datetime as dt
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import logs  # noqa: E402

TZ = dt.timezone(dt.timedelta(hours=8))
NOW = dt.datetime(2026, 11, 4, 20, 30, 0, tzinfo=TZ)


class BuildQueryTest(unittest.TestCase):
    def test_all_services_no_filter(self):
        self.assertEqual(logs.build_query([], []), '{product="wmesh"}')

    def test_service_alias_and_level_case(self):
        q = logs.build_query(logs.service_names("pad"), logs.level_names("warn"))
        self.assertEqual(q, '{product="wmesh", service="wmesh-pad", level="WARN"}')

    def test_multi_service_level(self):
        q = logs.build_query(logs.service_names("wan,factory"), logs.level_names("Warn,ERROR"))
        self.assertEqual(q, '{product="wmesh", service=~"wmesh-global|wmesh-factory", level=~"WARN|ERROR"}')

    def test_keywords_metadata_quoted(self):
        q = logs.build_query([], [], factory="f1", client='c"1', keywords=["/pad/assets", "400"],
                             excludes=["healthz"], regex=r"ms\":\d{3,}")
        self.assertEqual(
            q,
            '{product="wmesh"} |= "/pad/assets" |= "400" != "healthz" |~ "ms\\\\\\":\\\\d{3,}"'
            ' | factory="f1" | client="c\\"1"',
        )

    def test_unknown_level_rejected(self):
        with self.assertRaises(ValueError):
            logs.level_names("debug")
        self.assertEqual(logs.level_names("warning"), ["WARN"])


class TimeTest(unittest.TestCase):
    def test_since(self):
        self.assertEqual(logs.parse_since("90m"), dt.timedelta(minutes=90))
        self.assertEqual(logs.parse_since("1d"), dt.timedelta(days=1))
        with self.assertRaises(ValueError):
            logs.parse_since("1w")

    def test_parse_time_forms(self):
        self.assertEqual(logs.parse_time("19:48", NOW), dt.datetime(2026, 11, 4, 19, 48, tzinfo=TZ))
        self.assertEqual(logs.parse_time("11-03 19:48:05", NOW), dt.datetime(2026, 11, 3, 19, 48, 5, tzinfo=TZ))
        self.assertEqual(logs.parse_time("2026-11-03 19:48", NOW), dt.datetime(2026, 11, 3, 19, 48, tzinfo=TZ))
        self.assertEqual(logs.parse_time("2026-11-03T11:48:00+00:00", NOW).timestamp(),
                         dt.datetime(2026, 11, 3, 19, 48, tzinfo=TZ).timestamp())
        with self.assertRaises(ValueError):
            logs.parse_time("昨天", NOW)

    def test_range(self):
        a = argparse.Namespace(since="2h", frm=None, to=None)
        self.assertEqual(logs.time_range(a, NOW), (NOW - dt.timedelta(hours=2), NOW))
        a = argparse.Namespace(since="15m", frm="20:00", to="19:00")
        with self.assertRaises(ValueError):
            logs.time_range(a, NOW)

    def test_ns(self):
        t = dt.datetime(2026, 11, 4, 12, 0, 0, 123456, tzinfo=dt.timezone.utc)
        self.assertEqual(logs.ns(t) % 1_000_000_000, 123456000)


class SafetyTest(unittest.TestCase):
    def test_loki_url_only_local(self):
        self.assertTrue(logs.loki_url_ok("http://host.docker.internal:3100/loki/api/v1/push"))
        self.assertFalse(logs.loki_url_ok("http://154.82.81.16:3100/loki/api/v1/push"))
        self.assertFalse(logs.loki_url_ok("http://10.0.0.5:3100/loki/api/v1/push"))
        self.assertFalse(logs.loki_url_ok(""))

    def test_write_hosts_from_metrics(self):
        m = ('# HELP loki_write_sent_bytes_total x\n'
             'loki_write_sent_bytes_total{component_id="loki.write.loki",component_path="/",host="host.docker.internal:3100"} 10\n'
             'loki_write_dropped_entries_total{component_id="loki.write.loki",host="154.82.81.16:3100",reason="x"} 1\n'
             'other_metric{host="a"} 1\n')
        self.assertEqual(logs.write_hosts(m), {"host.docker.internal:3100", "154.82.81.16:3100"})


class FetchTest(unittest.TestCase):
    def test_paging_dedups_edge_and_honors_limit(self):
        pages = [
            [(1, "a"), (2, "b"), (3, "c")],
            [(3, "c"), (3, "d"), (4, "e")],
            [(4, "e")],
        ]
        calls = []

        def fake_get(base, path, params):
            calls.append(params["start"])
            vals = pages[len(calls) - 1]
            return {"data": {"result": [{"stream": {"service": "s"}, "values": [[str(t), l] for t, l in vals]}]}}

        old, logs.PAGE = logs.PAGE, 3
        orig = logs.loki_get
        logs.loki_get = fake_get
        try:
            got = [(t, l) for t, l, _ in logs.fetch("x", "q", 0, 10, 0)]
            self.assertEqual(got, [(1, "a"), (2, "b"), (3, "c"), (3, "d"), (4, "e")])
            self.assertEqual(calls, [0, 3, 4])
            calls.clear()
            got = [(t, l) for t, l, _ in logs.fetch("x", "q", 0, 10, 2)]
            self.assertEqual(got, [(1, "a"), (2, "b")])
        finally:
            logs.loki_get = orig
            logs.PAGE = old

    def test_paging_stops_when_page_is_all_seen_edge(self):
        # 同一纳秒的行挤满一页时，翻页起点不变、再拉还是这页，不能死循环。
        calls = []

        def fake_get(base, path, params):
            calls.append(params["start"])
            if len(calls) > 3:
                raise AssertionError("翻页死循环")
            return {"data": {"result": [{"stream": {}, "values": [["5", "a"], ["5", "b"]]}]}}

        old, logs.PAGE = logs.PAGE, 2
        orig = logs.loki_get
        logs.loki_get = fake_get
        try:
            got = [(t, l) for t, l, _ in logs.fetch("x", "q", 0, 10, 0)]
            self.assertEqual(got, [(5, "a"), (5, "b")])
            self.assertEqual(calls, [0, 5])
        finally:
            logs.loki_get = orig
            logs.PAGE = old

    def test_render_jsonl(self):
        line = logs.render(1_700_000_000_000_000_000, "hi", {"service": "wmesh-pad", "level": "INFO", "client": "c1"}, "jsonl")
        self.assertIn('"client": "c1"', line)
        self.assertIn('"line": "hi"', line)


if __name__ == "__main__":
    unittest.main()
