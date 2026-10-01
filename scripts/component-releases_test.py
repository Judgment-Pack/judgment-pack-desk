import importlib.util
import contextlib
import io
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
spec = importlib.util.spec_from_file_location('pins', Path(__file__).with_name('component-releases.py'))
pins = importlib.util.module_from_spec(spec); spec.loader.exec_module(pins)
class PinsTest(unittest.TestCase):
    def test_no_downgrade_or_preview_as_stable(self):
        current = {'version':'v2.0.0','channel':'stable'}
        for tag in ('v1.9.9','v2.0.0','v3.0.0-rc.1','main','v03.0.0'):
            self.assertFalse(pins.should_update(current,tag))
        self.assertTrue(pins.should_update(current,'v2.1.0'))
        self.assertTrue(pins.should_update({'version':'v2.0.0-rc.1','channel':'preview'},'v2.0.0'))
        self.assertFalse(pins.should_update({'version':'v2.0.0-dev+abc','channel':'development'},'v3.0.0'))
    def test_peels_annotated_tag_instead_of_branch(self):
        with patch.object(pins.subprocess,'check_output',return_value='a'*40+'\trefs/tags/v1.0.0\n'+'b'*40+'\trefs/tags/v1.0.0^{}\n'):
            self.assertEqual(pins.tag_commit('Judgment-Pack/judgment-pack-runner','v1.0.0'),'b'*40)
    def test_lock_is_supported(self): pins.read_plan()
    def verify(self, component, release, commit):
        with patch.object(pins, 'get_release', return_value=release) as lookup, patch.object(pins, 'tag_commit', return_value=commit) as tag, contextlib.redirect_stdout(io.StringIO()):
            pins.verify_plan({'components': {'runner': component}})
        return lookup, tag

    def component(self, channel='stable'):
        return {'repository': pins.REPOS['runner'], 'version': 'v1.0.0', 'revision': 'b' * 40, 'channel': channel}

    def test_rejects_stale_commit_even_if_version_is_published(self):
        with self.assertRaisesRegex(ValueError, 'disagrees with locked commit'):
            self.verify(self.component(), {'tag_name': 'v1.0.0', 'prerelease': False}, 'a' * 40)

    def test_rejects_missing_draft_or_different_release(self):
        for release in (None, {'tag_name': 'v1.0.0', 'draft': True}, {'tag_name': 'v2.0.0'}):
            with self.subTest(release=release), self.assertRaisesRegex(ValueError, 'Missing or mismatched'):
                self.verify(self.component(), release, 'b' * 40)

    def test_rejects_stable_shaped_tag_marked_prerelease(self):
        with self.assertRaisesRegex(ValueError, 'stable published release'):
            self.verify(self.component(), {'tag_name': 'v1.0.0', 'prerelease': True}, 'b' * 40)

    def test_verifies_exact_release_not_latest(self):
        lookup, tag = self.verify(self.component(), {'tag_name': 'v1.0.0', 'prerelease': False}, 'b' * 40)
        lookup.assert_called_once_with(pins.REPOS['runner'], 'v1.0.0')
        tag.assert_called_once_with(pins.REPOS['runner'], 'v1.0.0')

    def test_reviewed_preview_remains_valid(self):
        component = self.component('preview'); component['version'] = 'v1.0.0-rc.1'
        self.verify(component, {'tag_name': 'v1.0.0-rc.1', 'prerelease': True}, 'b' * 40)
        with patch.object(pins.subprocess, 'check_output', return_value='b' * 40 + '\trefs/tags/v1.0.0-rc.1\n'):
            self.assertEqual(pins.tag_commit(pins.REPOS['runner'], 'v1.0.0-rc.1'), 'b' * 40)

    def test_development_pin_does_not_assert_a_published_release(self):
        lookup, tag = self.verify(self.component('development'), None, None)
        lookup.assert_not_called(); tag.assert_not_called()

    def test_rejects_untrusted_repositories_and_refs_before_network(self):
        for repo, tag in (('someone/else', 'v1.0.0'), (pins.REPOS['runner'], 'main'), (pins.REPOS['runner'], 'v1.0.0/../../tags')):
            with self.subTest(repo=repo, tag=tag), patch.object(pins.urllib.request, 'urlopen') as http, patch.object(pins.subprocess, 'check_output') as git:
                with self.assertRaises(ValueError): pins.get_release(repo, tag)
                with self.assertRaises(ValueError): pins.tag_commit(repo, tag)
                http.assert_not_called(); git.assert_not_called()

class FreshnessTest(unittest.TestCase):
    def plan(self, version='v1.0.0', channel='stable'):
        return {'components': {'runner': {'repository': pins.REPOS['runner'], 'version': version, 'revision': 'b' * 40, 'channel': channel}}}

    def stable(self, tag):
        return {'tag_name': tag, 'draft': False, 'prerelease': False}

    def test_newer_stable_needs_attention_without_changing_the_plan(self):
        plan = self.plan()
        with patch.object(pins, 'get_release', return_value=self.stable('v1.0.1')) as lookup:
            rows, attention = pins.freshness(plan)
        self.assertTrue(attention)
        self.assertEqual(plan, self.plan())
        self.assertEqual(rows, [('runner', 'v1.0.0', 'v1.0.1', 'newer stable release not adopted')])
        lookup.assert_called_once_with(pins.REPOS['runner'])

    def test_matching_or_older_latest_is_not_an_update(self):
        for tag in ('v1.0.0', 'v0.9.0'):
            with self.subTest(tag=tag), patch.object(pins, 'get_release', return_value=self.stable(tag)):
                rows, attention = pins.freshness(self.plan())
            self.assertFalse(attention)
            self.assertEqual(rows[0][3], 'no newer stable release')

    def test_reviewed_preview_is_reported_once_its_stable_release_exists(self):
        with patch.object(pins, 'get_release', return_value=self.stable('v1.0.0')):
            self.assertTrue(pins.freshness(self.plan('v1.0.0-rc.1', 'preview'))[1])

    def test_development_pin_is_held_and_never_looked_up(self):
        with patch.object(pins, 'get_release') as lookup:
            rows, attention = pins.freshness(self.plan('v1.1.0-dev+abc', 'development'))
        self.assertFalse(attention)
        self.assertEqual(rows[0][2:], ('not checked', 'development pin held'))
        lookup.assert_not_called()

    def test_failed_or_unusable_lookup_never_claims_current(self):
        failures = [None, {}, [], {'tag_name': None, 'prerelease': False}, self.stable('main'),
                    {'tag_name': 'v1.0.0'}, {'tag_name': 'v2.0.0', 'prerelease': True},
                    {'tag_name': 'v2.0.0', 'draft': True, 'prerelease': False},
                    self.stable('v2.0.0 | injected'), self.stable('v2.0.0-rc.1')]
        for release in failures:
            with self.subTest(release=release), patch.object(pins, 'get_release', return_value=release):
                rows, attention = pins.freshness(self.plan())
            self.assertTrue(attention)
            self.assertEqual(rows[0][2:], ('unknown', 'lookup failed'))
        for error in (pins.urllib.error.URLError('offline'), ValueError('Oversized release metadata'), TimeoutError()):
            with self.subTest(error=error), patch.object(pins, 'get_release', side_effect=error):
                self.assertEqual(pins.freshness(self.plan())[0][0][3], 'lookup failed')

    def test_report_goes_to_the_job_summary_and_sets_the_exit_code(self):
        with tempfile.TemporaryDirectory() as temp:
            summary = Path(temp) / 'summary.md'
            for release, code in ((self.stable('v1.0.0'), 0), (self.stable('v1.0.1'), 1), (None, 1)):
                summary.write_text('')
                with self.subTest(release=release), patch.dict(os.environ, {'GITHUB_STEP_SUMMARY': str(summary)}), \
                     patch.object(pins, 'get_release', return_value=release), contextlib.redirect_stdout(io.StringIO()) as out:
                    self.assertEqual(pins.report_freshness(self.plan()), code)
                self.assertEqual(summary.read_text().strip(), out.getvalue().strip())
                self.assertIn('| runner | v1.0.0 |', summary.read_text())
                self.assertIn('does not change the lock', summary.read_text())

    def test_status_action_exits_non_zero_when_attention_is_needed(self):
        with patch.object(sys, 'argv', ['component-releases.py', 'status']), \
             patch.object(pins, 'read_plan', return_value=self.plan()), patch.object(pins, 'get_release', return_value=None), \
             contextlib.redirect_stdout(io.StringIO()), self.assertRaises(SystemExit) as exit:
            pins.main()
        self.assertEqual(exit.exception.code, 1)

if __name__=='__main__': unittest.main()
