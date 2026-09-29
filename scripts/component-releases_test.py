import importlib.util
import contextlib
import io
from pathlib import Path
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

if __name__=='__main__': unittest.main()
