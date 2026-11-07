import copy
import importlib.util
from pathlib import Path
import unittest
import tempfile
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('package_release',Path(__file__).with_name('package-release.py'))
p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)

class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.plan=copy.deepcopy(p.components.read_plan())
        for component in self.plan['components'].values():
            component.update(channel='stable',version='v0.4.0')
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
    def test_component_tag_must_match_commit(self):
        component=self.plan['components']['gateway'];sha=component['revision']
        with patch.object(p,'run'),patch.object(p.subprocess,'check_output',side_effect=[sha,'0'*40]):
            with self.assertRaisesRegex(ValueError,'tag disagrees'):p.source(component,Path('/unused-test-path'))
        with patch.object(p,'run'),patch.object(p.subprocess,'check_output',side_effect=[sha,sha]):
            p.source(component,Path('/unused-test-path'))
    def test_development_checkout_checks_commit_without_inventing_a_tag(self):
        component=dict(self.plan['components']['runtime'],channel='development',version='v0.33.0-dev.example')
        with patch.object(p,'run') as run,patch.object(p.subprocess,'check_output',return_value=component['revision']):
            p.source(component,Path('/unused-test-path'))
            self.assertFalse(any('refs/tags/' in str(call) for call in run.call_args_list))
        with patch.object(p,'run'),patch.object(p.subprocess,'check_output',return_value='0'*40):
            with self.assertRaisesRegex(ValueError,'checkout disagrees'):p.source(component,Path('/unused-test-path'))
    def test_linux_build_preserves_disabled_cgo_and_verified_runtime_digest(self):
        self.check_build('Linux', 'x86_64', 'linux/amd64', '0')

    def test_darwin_amd64_build_enables_cgo_and_embeds_verified_runtime_digest(self):
        self.check_build('Darwin', 'x86_64', 'darwin/amd64', '1')

    def test_darwin_arm64_build_enables_cgo_and_embeds_verified_runtime_digest(self):
        self.check_build('Darwin', 'arm64', 'darwin/arm64', '1')

    def check_build(self, system, machine, platform, cgo):
        class BuildObserved(Exception): pass
        order=[]
        expected_platform=platform
        os_name, arch=platform.split('/')
        def install(plan, platform, bundle, downloads):
            self.assertEqual(platform, expected_platform)
            self.assertIs(plan, self.plan)
            order.append('verified install')
            (bundle / 'jpack').write_bytes(b'abc')
        def run(args, **kwargs):
            if args[0] == 'npm':
                order.append('web build')
                return
            self.assertEqual(args[:4], ['go', 'build', '-trimpath', '-ldflags'])
            order.append('desk build')
            self.assertEqual(args[4], '-X github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan.Version=1.2.3 '
                             '-X github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan.runtimeArtifactSHA256='
                             'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
            self.assertEqual({k:kwargs['env'][k] for k in ['CGO_ENABLED','GOWORK','GOOS','GOARCH']},
                             {'CGO_ENABLED':cgo,'GOWORK':'off','GOOS':os_name,'GOARCH':arch})
            self.assertEqual(Path(args[6]).name, 'jpack-desk')
            self.assertEqual(args[7], '.')
            raise BuildObserved()
        with tempfile.TemporaryDirectory(prefix='desk-release-test-') as output:
            with patch('sys.argv', ['package-release.py','1.2.3','--output',output]), \
                 patch.object(p.components,'read_plan',return_value=self.plan), \
                 patch.object(p,'validate_release',return_value='a'*40), \
                 patch.object(p.components,'verify_plan',side_effect=lambda plan:order.append('verify plan')), \
                 patch.object(p.published,'install',side_effect=install), \
                 patch.object(p.platform,'system',return_value=system), \
                 patch.object(p.platform,'machine',return_value=machine), \
                 patch.object(p,'run',side_effect=run):
                with self.assertRaises(BuildObserved):p.main()
        self.assertEqual(order, ['verify plan','verified install','web build','desk build'])
if __name__=='__main__':unittest.main()
