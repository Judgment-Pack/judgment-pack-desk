import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('package_release',Path(__file__).with_name('package-release.py'))
p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)

class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.plan=copy.deepcopy(p.components.read_plan())
        self.plan['components']['gateway'].update(channel='stable',version='v0.4.0')
    def test_refuses_unpublished_components_before_any_build(self):
        self.plan['components']['gateway']['channel']='development'
        with patch.object(p.subprocess,'check_output') as git:
            with self.assertRaisesRegex(ValueError,'development'):p.validate_release('1.2.3',self.plan)
            git.assert_not_called()
    def test_refuses_dirty_tree_and_tag_mismatch(self):
        with patch.object(p.subprocess,'check_output',return_value=b' M source.go'):
            with self.assertRaisesRegex(ValueError,'clean'):p.validate_release('1.2.3',self.plan)
        with patch.object(p.subprocess,'check_output',side_effect=[b'', 'a'*40, 'b'*40]):
            with self.assertRaisesRegex(ValueError,'exact commit'):p.validate_release('1.2.3',self.plan)
    def test_clean_tagged_tree_and_invalid_version(self):
        with patch.object(p.subprocess,'check_output',side_effect=[b'', 'a'*40, 'a'*40]):
            self.assertEqual(p.validate_release('1.2.3',self.plan),'a'*40)
        for version in ['v1.2.3','1.2.3-rc.1','main','01.2.3']:
            with self.assertRaisesRegex(ValueError,'stable version'):p.validate_release(version,self.plan)
if __name__=='__main__':unittest.main()
