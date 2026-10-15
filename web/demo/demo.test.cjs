// DOM contract checks for the actual demo script, without browser dependencies.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, 'demo.js'), 'utf8');
const data = JSON.parse(fs.readFileSync(path.join(__dirname, 'data.json'), 'utf8'));
function element(dataset = {}) {
  return {dataset, attrs: {}, handlers: {}, textContent: '', innerHTML: '',
    setAttribute(k,v) {this.attrs[k]=v;},
    addEventListener(k,fn) {this.handlers[k]=fn;}, focus() {this.focused=true;}};
}
async function app(response = {ok:true, json:async () => data}) {
  const nodes = Object.fromEntries(['view','server','announcement','provenance'].map(id=>[id,element()]));
  nodes.server.value='17.6'; nodes.server.selectedIndex=0;
  nodes.server.options=[{text:'PostgreSQL 17.6'},{text:'PostgreSQL 16.10'}];
  const steps=['schema','plan','apply','verify'].map(step=>element({step}));
  const scenarios=['match','drift'].map(scenario=>element({scenario}));
  nodes.view.querySelectorAll=()=>scenarios;
  nodes.view.querySelector=s=>scenarios.find(e=>s.includes(e.dataset.scenario));
  const document={getElementById(id){
    if(id==='view-title') return {textContent:(nodes.view.innerHTML.match(/id="view-title">([^<]+)/)||[])[1]};
    return nodes[id];
  },querySelectorAll(){return steps;}};
  await vm.runInNewContext(source,{document,fetch:async url=>{assert.equal(url,'data.json');return response;}});
  return {nodes,steps,scenarios,click(name){steps.find(b=>b.dataset.step===name).handlers.click();}};
}
test('both server recordings render schema, six exact SQL statements, preserved rows and drift controls',async()=>{
  const a=await app();
  for(const [version,index] of [['17.6',0],['16.10',1]]) {
    a.nodes.server.value=version;a.nodes.server.selectedIndex=index;a.nodes.server.handlers.change();
    a.click('schema');assert.match(a.nodes.view.innerHTML,/customer_name/);assert.match(a.nodes.view.innerHTML,/customer_id/);
    a.click('plan');assert.equal((a.nodes.view.innerHTML.match(/class="op"/g)||[]).length,6);
    assert.match(a.nodes.view.innerHTML,new RegExp(data.runs[version].current.schema));
    assert.match(a.nodes.view.innerHTML,/ALTER TABLE/);assert.match(a.nodes.view.innerHTML,/CREATE INDEX/);
    a.click('apply');assert.match(a.nodes.view.innerHTML,/Ada Example/);assert.match(a.nodes.view.innerHTML,/Dana Example/);
    assert.match(a.nodes.view.innerHTML,/same plan/);assert.match(a.nodes.view.innerHTML,/NULL/);
    a.click('verify');assert.match(a.nodes.view.innerHTML,/MATCH · EXIT 0/);
    a.scenarios[1].handlers.click();assert.match(a.nodes.view.innerHTML,/DRIFT · EXIT 4/);
    assert.match(a.nodes.view.innerHTML,/column_unexpected/);assert.match(a.nodes.announcement.textContent,/drift detected/);
    assert.equal(a.scenarios[1].focused,true);
    a.scenarios[0].handlers.click();assert.match(a.nodes.view.innerHTML,/MATCH · EXIT 0/);
    assert.equal(a.steps.find(b=>b.dataset.step==='verify').attrs['aria-pressed'],'true');
  }
});
test('artifact downloads exist for both versions and no arbitrary SQL is sent over the network',async()=>{
  const a=await app();
  for(const version of ['17.6','16.10']) {
    a.nodes.server.value=version;
    for(const step of ['schema','plan','apply','verify']) {
      a.click(step);
      for(const m of a.nodes.view.innerHTML.matchAll(/href="artifacts\/postgres-([\d.]+)\/([\w.-]+)"/g)) {
        assert.equal(m[1],version);
        assert.ok(fs.existsSync(path.join(__dirname,'../../examples/walkthrough','postgres-'+m[1],m[2])));
      }
    }
  }
  assert.equal((source.match(/fetch\(/g)||[]).length,1);
});
test('loading failures offer an artifact fallback',async()=>{
  const a=await app({ok:false});
  assert.match(a.nodes.view.innerHTML,/could not load/);
  assert.match(a.nodes.view.innerHTML,/github.com\/ForgeSchema/);
});
