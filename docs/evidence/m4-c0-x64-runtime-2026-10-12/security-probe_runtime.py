"""Adversarial data joins using fake Docker/Linux and temporary private inputs only."""
import copy
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import Mock, patch

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
SOURCE = ROOT/'snapshot/tests/crawl-jobs-v2-observer'
OUT = ROOT/'security'
sys.path.insert(0, str(SOURCE))
import contracts as c
import runtime_admission as admission
import runtime_context as ctx
import context_provision as provision
import context_transport as transport
import profiles
import linux_observation
import build_oracle
from streams import Deadline, WireBudget
from test_admission_dispatch import PipePair
import test_runtime_admission as native
import test_runtime_context as context_fixture
import test_context_provisioning as provision_fixture
from test_architecture_contracts import header

report = {'kind':'independent_runtime_security_probes','native_execution':False,'real_docker_or_host_collector':False,
          'accepted':[], 'refusals':[], 'status':'RUNNING'}

def refuse(name, fn):
    try: fn()
    except c.Invalid as error:
        code = str(error)
        assert 'private-canary' not in code
        report['refusals'].append({'name':name,'code':code})
    else: raise AssertionError('unexpected acceptance: '+name)

def context_parts(arch):
    parts = list(context_fixture.fixture())
    parts[0].value['architecture'] = parts[2]['architecture'] = arch
    parts[3] = c.preparation(parts[3]['artifacts'],arch)
    parts[4] = ctx.LinuxContextEndpoint(parts[2],context_fixture.KEY,parts[0])
    parts[5] = ctx.ContextSource(parts[2],context_fixture.KEY,parts[4].exchange,authorize=lambda *_: True)
    return parts

for arch, other in [('aarch64','x86_64'),('x86_64','aarch64')]:
    prep, context = native.preparation(arch), native.context(arch)
    bound = native.binding() | {'architecture':arch}
    peer = admission.validate(native.frames(bound=bound),bound,prep,context)
    for role in ('target','observer','oracle'):
        bound = native.binding(role) | {'architecture':arch}
        raw = native.frames(role,bound=bound)
        kw = {} if role == 'target' else {'target':peer}
        valid = admission.validate(raw,bound,prep,context,**kw)
        assert not valid['execution_authorized'] and not valid['runtime_evidence']
        report['accepted'].append(arch+':native:'+role)
        for location in ('preparation','context','binding','frame','elf'):
            p,ctx_input,b,r = copy.deepcopy(prep),copy.deepcopy(context),dict(bound),list(raw)
            if location == 'preparation': p = c.preparation(p['artifacts'],other)
            elif location == 'context': ctx_input['architecture'] = other
            elif location == 'binding': b['architecture'] = other
            elif location == 'frame': r = native.changed(r,0,architecture=other)
            else: r = native.changed(r,0,executable_sha256=c.sha(b'foreign-elf'))
            refuse(arch+':'+role+':foreign-'+location,lambda:admission.validate(r,b,p,ctx_input,**kw))
        if role != 'target':
            poisoned = copy.deepcopy(peer); poisoned['binding']['architecture'] = other
            refuse(arch+':'+role+':foreign-peer',lambda:admission.validate(raw,bound,prep,context,target=poisoned))
        for index, update in [(0,{'pid':True}),(0,{'version':1}),(1,{'seccomp_filters':16}),(1,{'caps':[0,0,0,0,1]}),
                              (1,{'uids':[999,0,999,999]}),(2,{'memory_max':2**53-1}),(3,{'monotonic_ns':context['not_after_ns']+1}),
                              (3,{'monotonic_ns':2**64}),(3,{'monotonic_ns':True})]:
            r = native.changed(raw,index,**update)
            refuse(arch+':'+role+':invalid-frame-'+str(index)+'-'+next(iter(update)),lambda:admission.validate(r,bound,prep,context,**kw))

    parts = context_parts(arch)
    lease = context_fixture.acquire(parts)
    assert lease.view('P1.pre.1.1',c.sha(b'scope'),parts[3],parts[6])['architecture'] == arch
    assert not lease.receipt['execution_authorized'] and not lease.receipt['runtime_evidence']
    report['accepted'].append(arch+':authenticated-context')
    refuse(arch+':lease-foreign-prep',lambda:lease.view('P1.pre.1.1',c.sha(b'scope'),c.preparation(parts[3]['artifacts'],other),parts[6]))
    parts[1].now = lease.end
    refuse(arch+':lease-exact-expiry',lambda:lease.check(parts[6]))

    for bad in ('missing-authorization','truthy-authorization','wrong-daemon','wrong-source','foreign-preparation','legacy-anchor'):
        parts = context_parts(arch)
        calls = []; parts[5].exchange = lambda *_: calls.append(True)
        if bad == 'missing-authorization': parts[5].authorize = None
        if bad == 'truthy-authorization': parts[5].authorize = lambda *_: 1
        if bad == 'wrong-daemon': parts[5].anchor['daemon_sha256'] = c.sha(b'wrong')
        if bad == 'wrong-source': parts[3]['artifacts']['runtime_context_source'] = c.sha(b'wrong')
        if bad == 'foreign-preparation': parts[3] = c.preparation(parts[3]['artifacts'],other)
        if bad == 'legacy-anchor': parts[5].anchor['version'] = 1
        refuse(arch+':context-before-io:'+bad,lambda:context_fixture.acquire(parts))
        assert calls == []

    for fault in ('foreign-architecture','legacy','foreign-request','window-growth','stale-sample','foreign-boot','foreign-namespace','extra-field','missing-field','bad-mac','reorder','duplicate','oversize','delay'):
        parts = context_parts(arch)
        original = parts[5].exchange
        def altered(raw,deadline):
            frames = original(raw,deadline).splitlines(keepends=True)
            values = [c.decode(v) for v in frames]
            if fault == 'foreign-architecture': values[0]['payload']['architecture'] = other
            elif fault == 'legacy': values[0]['version'] = 1
            elif fault == 'foreign-request': values[0]['request_sha256'] = c.sha(b'foreign')
            elif fault == 'window-growth': values[2]['payload']['not_after_ns'] += 1
            elif fault == 'stale-sample': values[2]['payload']['sampled_ns'] = values[2]['payload']['not_after_ns']
            elif fault == 'foreign-boot': values[0]['payload']['kernel_boot_sha256'] = c.sha(b'foreign')
            elif fault == 'foreign-namespace': values[1]['payload']['host_pid_ns'] += 1
            elif fault == 'extra-field': values[0]['payload']['private-canary'] = True
            elif fault == 'missing-field': del values[0]['payload']['architecture']
            elif fault == 'delay': parts[1].now += 3
            frames = [c.canonical(v) for v in values]
            if fault == 'reorder': frames[0],frames[1] = frames[1],frames[0]
            if fault == 'duplicate': frames[1] = frames[0]
            seal = values[-1]
            seal['mac'] = ctx.mac(context_fixture.KEY,raw+b''.join(frames[:-1])) if fault != 'bad-mac' else c.sha(b'wrong-key')
            frames[-1] = c.canonical(seal)
            return b'x'*4097 if fault == 'oversize' else b''.join(frames)
        parts[5].exchange = altered
        refuse(arch+':signed-context:'+fault,lambda:context_fixture.acquire(parts))

    parts = context_parts(arch); captured = []; original = parts[5].exchange
    def save(raw,deadline):
        captured.append(original(raw,deadline)); return captured[-1]
    parts[5].exchange = save
    context_fixture.acquire(parts)
    parts[5].exchange = lambda *_: captured[0]
    refuse(arch+':context-replay',lambda:context_fixture.acquire(parts))
    for when in ('authorization','exchange'):
        parts = context_parts(arch)
        original = parts[5].exchange
        def mutate_auth(*_):
            parts[5].anchor['architecture'] = other
            return True
        def mutate_exchange(raw,deadline):
            received = original(raw,deadline)
            parts[5].anchor['architecture'] = other
            return received
        if when == 'authorization': parts[5].authorize = mutate_auth
        else: parts[5].exchange = mutate_exchange
        refuse(arch+':anchor-mutation:'+when,lambda:context_fixture.acquire(parts))

    deployment = provision_fixture.deployment()
    deployment['architecture'] = deployment['anchor']['architecture'] = arch
    for where in ('deployment','anchor'):
        changed = copy.deepcopy(deployment)
        (changed if where == 'deployment' else changed['anchor'])['architecture'] = other
        refuse(arch+':mixed-deployment:'+where,lambda:provision.validate_deployment(changed))
    with tempfile.TemporaryDirectory(prefix='bundle-') as folder:
        bundle = Path(folder)/'bundle'
        record = provision.create_bundle(bundle,deployment,provision_fixture.identity_bytes(),key=context_fixture.KEY)
        assert record['architecture'] == arch
        p = native.preparation(arch)
        p['artifacts'].update(runtime_context_deployment=provision.deployment_digest(deployment),
                              runtime_context_transport=provision.transport_digest(),runtime_context_source=deployment['anchor']['source_sha256'])
        loaded = transport.provisioned_source(bundle/'controller',p,authorize=lambda *_: True)
        report['accepted'].append(arch+':provisioning-bundle')
        for field in ('runtime_context_deployment','runtime_context_transport','runtime_context_source'):
            changed = copy.deepcopy(p); changed['artifacts'][field] = c.sha(b'foreign')
            refuse(arch+':bundle-substitution:'+field,lambda:transport.provisioned_source(bundle/'controller',changed,authorize=lambda *_:True))
        refuse(arch+':bundle-foreign-preparation',lambda:transport.provisioned_source(bundle/'controller',c.preparation(p['artifacts'],other),authorize=lambda *_: True))

    # Before-create AND direct-before-start image admission, for every role.
    for role in ('target','observer','oracle'):
        for operation in ('create','start'):
            for fault in ('none','foreign','missing-arch','alias','x32','wrong-os','wrong-id','missing-image','missing-config','onbuild','healthcheck','environment','volumes'):
                with PipePair(architecture=arch) as pair:
                    pair.deadlines['oracle'] = Deadline(5)
                    expected = pair.registry.spec(role)
                    ref = pair.registry.entries[role]['reference']
                    if role == 'observer':
                        pair.fake.containers[pair.registry.entries['target']['reference']['id']]['State'].update(Running=True,Pid=101,Status='running')
                    orig_inspect = pair.fake.inspect
                    def inspect(kind,selector,deadline):
                        value = orig_inspect(kind,selector,deadline)
                        if kind != 'image': return value
                        if fault == 'foreign': value['Architecture'] = c.platform(other)['image_architecture']
                        elif fault == 'missing-arch': del value['Architecture']
                        elif fault == 'alias': value['Architecture'] = arch
                        elif fault == 'x32': value['Architecture'] = 'x32'
                        elif fault == 'wrong-os': value['Os'] = 'windows'
                        elif fault == 'wrong-id': value['Id'] = 'sha256:'+c.sha(b'foreign')
                        elif fault == 'missing-image': return None
                        elif fault == 'missing-config': del value['Config']
                        elif fault == 'onbuild': value['Config']['OnBuild'] = ['RUN private-canary']
                        elif fault == 'healthcheck': value['Config']['Healthcheck'] = {'Test':['CMD','private-canary']}
                        elif fault == 'environment': value['Config']['Env'] = ['private-canary=1']
                        elif fault == 'volumes': value['Config']['Volumes'] = {'/private-canary':{}}
                        return value
                    pair.fake.inspect = inspect
                    spawn = Mock(return_value=SimpleNamespace())
                    pair.backend.stream_factory = spawn
                    mutations = []
                    original_success = pair.backend._success
                    def success(args,deadline):
                        if args[:2] == ['container','create']:
                            mutations.append(args); return ref['id'].encode()
                        return original_success(args,deadline)
                    pair.backend._success = success
                    profile_path = pair.root/'profile.json'
                    profile_path.write_bytes(c.canonical(profiles.profile(role,arch)))
                    pair.backend.profile_files[role] = profile_path
                    if operation == 'start':
                        action = lambda:pair.backend.open_actor(ref,expected,pair.registry.snapshot(),pair.budget,pair.deadlines[role])
                    else: action = lambda:pair.backend.create_container(expected,Deadline(5))
                    name = arch+':'+role+':'+operation+':image-'+fault
                    if fault == 'none':
                        action(); report['accepted'].append(name)
                        assert (spawn.call_count == 1) if operation == 'start' else len(mutations) == 1
                    else:
                        refuse(name,action)
                        assert not spawn.called and not mutations

        with PipePair(architecture=arch) as pair:
            pair.deadlines['oracle'] = Deadline(5)
            pair.backend.authorize = None
            with patch.object(pair.backend,'stream_factory') as spawn:
                refuse(arch+':'+role+':unapproved-start',lambda:pair.backend.open_actor(pair.registry.entries[role]['reference'],pair.registry.spec(role),pair.registry.snapshot(),pair.budget,pair.deadlines[role]))
                assert not pair.events and not spawn.called
        with PipePair(architecture=arch) as pair:
            pair.deadlines['oracle'] = Deadline(5)
            ref = pair.registry.entries[role]['reference']
            if role == 'observer':
                pair.fake.containers[pair.registry.entries['target']['reference']['id']]['State'].update(Running=True,Pid=101,Status='running')
            pair.fake.containers[ref['id']]['HostConfig']['SecurityOpt'][1] = 'seccomp='+json.dumps(profiles.profile(role,other))
            with patch.object(pair.backend,'stream_factory') as spawn:
                refuse(arch+':'+role+':inspected-foreign-profile',lambda:pair.backend.open_actor(ref,pair.registry.spec(role),pair.registry.snapshot(),pair.budget,pair.deadlines[role]))
                assert not spawn.called

    # Oracle ABI observed from a stubbed uname, joined to actual private ELF bytes.
    with tempfile.TemporaryDirectory(prefix='oracle-elf-') as folder:
        path = Path(folder)/'python'; real_open, real_read = os.open,os.read
        for elf_arch in (arch,other):
            raw = header(elf_arch)+b'inert-private-fixture'; path.write_bytes(raw)
            with patch.object(linux_observation.os,'uname',return_value=SimpleNamespace(sysname='Linux',machine=arch)), \
                 patch.object(linux_observation.os,'open',side_effect=lambda *_: real_open(path,os.O_RDONLY)), \
                 patch.object(linux_observation.os,'read',side_effect=lambda fd,n: real_read(fd,min(n,1))):
                reader = linux_observation.LinuxReader.native_process()
                if elf_arch == arch:
                    assert reader.executable() == c.sha(raw)
                    report['accepted'].append(arch+':observed-fragmented-oracle-elf')
                else: refuse(arch+':foreign-observed-interpreter',reader.executable)

for system,machine,size in [('Linux','x32',4),('Linux','x86_64',4),('Linux','amd64',8),('Linux','i686',8),('Darwin','x86_64',8)]:
    with patch.object(linux_observation.os,'uname',return_value=SimpleNamespace(sysname=system,machine=machine)), \
         patch.object(linux_observation.ctypes,'sizeof',return_value=size):
        refuse('observed-abi:'+system+':'+machine+':'+str(size),linux_observation.LinuxReader.native_process)

report['status'] = 'PASS'
(OUT/'runtime-probes.json').write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
print(json.dumps({'status':report['status'],'positive_controls':len(report['accepted']),'refusals':len(report['refusals'])}))
