import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('updater',Path(__file__).with_name('desk-update.py'))
u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)

class UpdatesTest(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)/'install';u.installation(self.root,True)
    def bundle(self, version='1.0.0'):
        folder=self.root/'releases'/version;folder.mkdir()
        names=['jpack-desk','jpack','jpack-runner','jpack-source-worker','gateway','gateway-bundle.json']
        for name in names: (folder/name).write_text('binary '+name)
        manifest={'version':version,'platform':u.host_platform(),'stateEpoch':1,'desk':{'repository':'https://github.com/'+u.REPO},'files':{name:u.digest_file(folder/name) for name in names}}
        (folder/'release-manifest.json').write_text(json.dumps(manifest))
        return folder,{'version':version,'manifestDigest':u.digest_file(folder/'release-manifest.json')}
    def test_stage_is_not_activation_and_rollback_keeps_data(self):
        first,a=self.bundle();second,b=self.bundle('2.0.0')
        data=Path(self.temp.name)/'desk-data';data.write_text('pinned runtime and jobs')
        state=u.read_state(self.root);state.update(current=a,pending=b,autoInstall=True);u.atomic_json(self.root/'installation.json',state)
        self.assertEqual(u.read_state(self.root)['current'],a)
        self.assertEqual(u.activate(self.root)['current'],b)
        restored=u.activate(self.root,True)
        self.assertEqual(restored['current'],a)
        self.assertFalse(restored['autoInstall'])
        self.assertEqual(data.read_text(),'pinned runtime and jobs')
        self.assertTrue(first.exists());self.assertTrue(second.exists())
    def test_activation_updates_launcher_from_the_verified_release(self):
        folder,selected=self.bundle()
        helper=folder/'desk-update.py';helper.write_text('# reviewed update protocol 1\n')
        manifest=folder/'release-manifest.json';doc=json.loads(manifest.read_text())
        doc['files']['desk-update.py']=u.digest_file(helper);manifest.write_text(json.dumps(doc))
        selected['manifestDigest']=u.digest_file(manifest)
        state=u.read_state(self.root);state['pending']=selected;u.atomic_json(self.root/'installation.json',state)
        u.activate(self.root)
        self.assertEqual((self.root/'desk-update.py').read_bytes(),helper.read_bytes())
        self.assertEqual((self.root/'desk-update.py').stat().st_mode & 0o777,0o700)
    def test_tampering_never_changes_current(self):
        first,a=self.bundle();second,b=self.bundle('2.0.0')
        state=u.read_state(self.root);state.update(current=a,pending=b);u.atomic_json(self.root/'installation.json',state)
        (second/'jpack').write_text('tampered')
        with self.assertRaises(ValueError):u.activate(self.root)
        self.assertEqual(u.read_state(self.root)['current'],a)
    def test_refuses_links_extra_files_and_data_migrations(self):
        folder,selected=self.bundle()
        (folder/'extra').write_text('not listed')
        with self.assertRaisesRegex(ValueError,'unlisted'):u.verify(folder,selected)
        (folder/'extra').unlink();(folder/'extra').symlink_to(folder/'jpack')
        with self.assertRaises(ValueError):u.verify(folder,selected)
        (folder/'extra').unlink()
        doc=json.loads((folder/'release-manifest.json').read_text());doc['stateEpoch']=2
        (folder/'release-manifest.json').write_text(json.dumps(doc));selected['manifestDigest']=u.digest_file(folder/'release-manifest.json')
        with self.assertRaisesRegex(ValueError,'migration'):u.verify(folder,selected)
    def test_archive_traversal_duplicate_and_links(self):
        for name,kind in [('../escape',tarfile.REGTYPE),('/escape',tarfile.REGTYPE),('link',tarfile.SYMTYPE),('file',tarfile.LNKTYPE)]:
            archive=Path(self.temp.name)/'archive.tar.gz'
            with tarfile.open(archive,'w:gz') as out:
                item=tarfile.TarInfo(name);item.type=kind;item.linkname='../escape';out.addfile(item)
            with self.assertRaises(ValueError):u.unpack(archive,Path(self.temp.name)/'out')
        self.assertFalse((Path(self.temp.name)/'escape').exists())
    def test_running_lock_refuses_a_second_launch_or_switch(self):
        with u.lock(self.root,'running.lock'):
            with self.assertRaisesRegex(ValueError,'running'):
                with u.lock(self.root,'running.lock'):self.fail('acquired twice')
    def test_failed_check_preserves_last_known_release_and_reports_error(self):
        state=u.read_state(self.root);state['latest']={'version':'1.0.0'};u.atomic_json(self.root/'installation.json',state)
        with patch.object(u,'latest_release',side_effect=TimeoutError()):out=u.checked(self.root,True)
        self.assertEqual(out['latest']['version'],'1.0.0');self.assertIn('error',out)
    def test_refuses_existing_checkout_as_installation(self):
        other=Path(self.temp.name)/'checkout';other.mkdir(mode=0o700);(other/'important').write_text('work')
        with self.assertRaises(ValueError):u.installation(other,True)
        self.assertEqual((other/'important').read_text(),'work')
    def test_verified_download_stages_without_switching_active_release(self):
        folder,selected=self.bundle('2.0.0')
        archive=Path(self.temp.name)/'bundle.tar.gz'
        with tarfile.open(archive,'w:gz') as out:
            for file in folder.iterdir():out.add(file,arcname=file.name)
        checksum=u.digest_file(archive);name='judgment-pack-desk_2.0.0_'+u.host_platform().replace('/','_')+'.tar.gz'
        release={'version':'2.0.0','tag':'v2.0.0','platform':u.host_platform(),'asset':name,'assetDigest':'sha256:'+checksum}
        class Reply(io.BytesIO):url='https://release-assets.githubusercontent.com/example'
        with patch.object(u,'latest_release',return_value=release),patch.object(u,'request',return_value=(checksum+'  '+name+'\n').encode()),patch.object(u.urllib.request,'urlopen',return_value=Reply(archive.read_bytes())):
            state=u.stage(self.root)
        self.assertIsNone(state['current']);self.assertEqual(state['pending'],selected)
    def test_launch_uses_verified_bundled_binaries(self):
        folder,selected=self.bundle();state=u.read_state(self.root);state.update(current=selected,checkedAt=u.time.time());u.atomic_json(self.root/'installation.json',state)
        with patch.object(u.subprocess,'Popen') as start,patch.object(u,'request',side_effect=AssertionError('manual launch must not wait on the network')):
            start.return_value.wait.return_value=0
            self.assertEqual(u.launch(self.root,['--','/project']),0)
            self.assertEqual(start.call_args.args[0],[str(folder/'jpack-desk'),'--jpack',str(folder/'jpack'),'--runner',str(folder/'jpack-runner'),'/project'])
        for override in ['--jpack','-jpack','--runner=/other','-runner=/other','-dev-token=secret','--local-gateway-worker']:
            with self.assertRaisesRegex(ValueError,'overrides'):u.launch(self.root,[override])
    def test_selects_only_the_native_archive(self):
        assets=[{'name':'judgment-pack-desk_2.0.0_'+p+'.tar.gz','digest':'sha256:'+'a'*64}
                for p in ['linux_amd64','darwin_arm64','darwin_amd64']]
        reply=json.dumps({'tag_name':'v2.0.0','assets':assets}).encode()
        for system, machine, expected in [('Linux','x86_64','linux/amd64'),('Darwin','arm64','darwin/arm64'),('Darwin','x86_64','darwin/amd64')]:
            with self.subTest(platform=expected), patch.object(u.platform,'system',return_value=system), patch.object(u.platform,'machine',return_value=machine), patch.object(u,'request',return_value=reply):
                release=u.latest_release()
                self.assertEqual(release['platform'],expected)
                self.assertEqual(release['asset'],'judgment-pack-desk_2.0.0_'+expected.replace('/','_')+'.tar.gz')
        with patch.object(u.platform,'system',return_value='Darwin'), patch.object(u.platform,'machine',return_value='arm64'), patch.object(u,'request',return_value=json.dumps({'tag_name':'v2.0.0','assets':[assets[0],assets[2]]}).encode()):
            self.assertIsNone(u.latest_release()['asset'])

    def test_foreign_platform_refused_on_verify_activation_rollback_and_launch(self):
        folder,selected=self.bundle()
        native=u.host_platform()
        foreign='darwin/arm64' if native!='darwin/arm64' else 'darwin/amd64'
        doc=json.loads((folder/'release-manifest.json').read_text());doc['platform']=foreign
        (folder/'release-manifest.json').write_text(json.dumps(doc));selected['manifestDigest']=u.digest_file(folder/'release-manifest.json')
        state=u.read_state(self.root);state.update(pending=selected,previous=selected);u.atomic_json(self.root/'installation.json',state)
        for action in [lambda:u.verify(folder,selected),lambda:u.verify(folder,selected,foreign),lambda:u.activate(self.root),lambda:u.activate(self.root,True)]:
            with self.assertRaisesRegex(ValueError,'Wrong release platform'):action()
            self.assertIsNone(u.read_state(self.root)['current'])
        state.update(current=selected,pending=None);u.atomic_json(self.root/'installation.json',state)
        with patch.object(u.subprocess,'Popen') as start:
            with self.assertRaisesRegex(ValueError,'Wrong release platform'):u.launch(self.root,[])
            start.assert_not_called()

    def test_wrong_platform_download_never_stages(self):
        folder,selected=self.bundle('2.0.0')
        doc=json.loads((folder/'release-manifest.json').read_text())
        doc['platform']='darwin/amd64' if u.host_platform()!='darwin/amd64' else 'linux/amd64'
        (folder/'release-manifest.json').write_text(json.dumps(doc))
        archive=Path(self.temp.name)/'foreign.tar.gz'
        with tarfile.open(archive,'w:gz') as out:
            for file in folder.iterdir():out.add(file,arcname=file.name)
        checksum=u.digest_file(archive);name='judgment-pack-desk_2.0.0_'+u.host_platform().replace('/','_')+'.tar.gz'
        release={'version':'2.0.0','tag':'v2.0.0','platform':u.host_platform(),'asset':name,'assetDigest':'sha256:'+checksum}
        class Reply(io.BytesIO):url='https://release-assets.githubusercontent.com/example'
        with patch.object(u,'latest_release',return_value=release),patch.object(u,'request',return_value=(checksum+'  '+name+'\n').encode()),patch.object(u.urllib.request,'urlopen',return_value=Reply(archive.read_bytes())):
            with self.assertRaisesRegex(ValueError,'Wrong release platform'):u.stage(self.root)
        self.assertIsNone(u.read_state(self.root)['pending'])
        self.assertIsNone(u.read_state(self.root)['current'])

if __name__=='__main__':unittest.main()
