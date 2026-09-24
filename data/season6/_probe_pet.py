import json
d = json.load(open('42_item_options.json', encoding='utf-8'))
byt = {t['id']: t['kind'] for t in d['option_types']}
PET = {'BlueFenrir', 'BlackFenrir', 'GoldFenrir', 'DarkHorse'}
petdefs = set()
for defn in d['definitions']:
    kinds = {byt.get(o.get('option_type'), '?') for o in defn.get('possible_options', [])}
    if kinds & PET:
        petdefs.add(defn['id'])
        print('DEF', defn['id'], repr(defn.get('name')), 'random', defn.get('adds_randomly'), 'chance', defn.get('add_chance'), 'maxopt', defn.get('maximum_options_per_item'))
        for o in defn.get('possible_options', []):
            pu = o.get('power_up') or {}
            boost = pu.get('boost') or {}
            rel = [(r.get('input', {}).get('designation'), r.get('input_operand')) for r in boost.get('related', [])]
            print('   opt#', o.get('number'), byt.get(o.get('option_type')), 'lt', o.get('level_type'), '->', pu.get('target'), boost.get('constant'), boost.get('aggregate_type'), 'rel', rel)
print('--- item entries referencing those defs ---')
for e in d['items']:
    if set(e.get('option_definitions', [])) & petdefs:
        print('item', e.get('group'), e.get('number'), e.get('option_definitions'))
print('--- combination bonuses with fenrir ---')
for b in d.get('combination_bonuses', []):
    reqs = [(byt.get(r.get('option_type'), r.get('option_type')), r.get('sub_option_type'), r.get('minimum_count')) for r in b.get('requirements', [])]
    if any(r[0] in PET for r in reqs):
        bn = b.get('bonus') or {}
        bb = bn.get('boost') or {}
        print('CB#', b.get('number'), repr(b.get('description')), 'multi', b.get('applies_multiple_times'), 'reqs', reqs, '->', bn.get('target'), bb.get('constant'), bb.get('aggregate_type'))
