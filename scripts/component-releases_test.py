import importlib.util
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
if __name__=='__main__': unittest.main()
