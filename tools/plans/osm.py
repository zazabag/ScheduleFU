"""Контуры корпусов из OpenStreetMap → данные плана в духе IMDF.

Разовый инструмент. Берёт ответ Overpass по корпусам Ленинградского кампуса
(отношения-мультиполигоны и их линии), склеивает линии в кольца и пишет
GeoJSON в формате Apple IMDF (открытый стандарт, OGC Community Standard):
venue — кампус, building и footprint — корпуса, level — этажи.

Почему IMDF: это простой GeoJSON с понятными типами (этаж, помещение,
проём), и обведённые потом по фото планов эвакуации аудитории ложатся в
него как unit с номером в alt_name — без переделки формата.

Помещений здесь пока нет, и это сознательно: внутренних планов в открытом
доступе нет, а угаданные места путали больше, чем помогали. Появятся —
когда аудитории будут обведены по фото (docs/08-plan-funkciy.md § 6).

Данные OSM — © участники OpenStreetMap, ODbL; ссылка обязательна и стоит
под планом.

Запуск:
    python tools/plans/osm.py ответ_overpass.json выход.geojson
Запрос Overpass (любое зеркало, например maps.mail.ru/osm/tools/overpass):
    rel(id:7896779,7932081,7932084,7932077,7932074,7896706);out body;
    way(r);out geom;way(552768384);out geom tags;
"""

import json
import sys

# Корпус → как он зовётся у нас и сколько в нём этажей (из OSM,
# building:levels). Ключ building — тот же, что в модуле campus.
CORPS = {
    ("relation", 7896779): {"id": "49", "name": "49 и 49/2", "levels": 5},
    ("relation", 7932081): {"id": "51-1", "name": "51 к.1", "levels": 11},
    ("relation", 7932084): {"id": "51-2", "name": "51 к.2", "levels": 5},
    ("relation", 7932077): {"id": "51-3", "name": "51 к.3", "levels": 9},
    ("way", 552768384): {"id": "51-4", "name": "51 с.4", "levels": 6},
    ("relation", 7932074): {"id": "53", "name": "53", "levels": 9},
    ("relation", 7896706): {"id": "55", "name": "55", "levels": 9},
}


def rings(way_geoms, members, role):
    """Склеивает линии отношения в замкнутые кольца. Линии в OSM идут в
    любом направлении, поэтому стыкуем по совпадающим концам, при нужде
    разворачивая."""
    segs = [list(way_geoms[m["ref"]]) for m in members if m["role"] == role and m["ref"] in way_geoms]
    out = []
    while segs:
        ring = segs.pop(0)
        changed = True
        while ring[0] != ring[-1] and changed:
            changed = False
            for i, s in enumerate(segs):
                if s[0] == ring[-1]:
                    ring += s[1:]
                elif s[-1] == ring[-1]:
                    ring += s[::-1][1:]
                elif s[-1] == ring[0]:
                    ring = s[:-1] + ring
                elif s[0] == ring[0]:
                    ring = s[::-1][:-1] + ring
                else:
                    continue
                segs.pop(i)
                changed = True
                break
        if ring[0] == ring[-1] and len(ring) >= 4:
            out.append(ring)
    return out


def main(src, dst):
    d = json.load(open(src, encoding="utf-8"))
    way_geoms = {}
    for e in d["elements"]:
        if e["type"] == "way" and "geometry" in e:
            way_geoms[e["id"]] = [(round(p["lon"], 7), round(p["lat"], 7)) for p in e["geometry"]]

    features = [{
        "type": "Feature", "id": "venue-leningradsky", "feature_type": "venue",
        "geometry": None,
        "properties": {"category": "university", "name": {"ru": "Ленинградский кампус"},
                       "address": "Ленинградский проспект, 49–55, Москва",
                       "source": "© участники OpenStreetMap, ODbL"},
    }]
    for e in d["elements"]:
        corp = CORPS.get((e["type"], e["id"]))
        if not corp:
            continue
        if e["type"] == "way":
            polys = [[way_geoms[e["id"]]]]
        else:
            outer = rings(way_geoms, e["members"], "outer")
            inner = rings(way_geoms, e["members"], "inner")
            polys = [[o] for o in outer]
            # Внутренние кольца (дворы) — к тому внешнему, куда попадает их
            # первая точка; корпусов с несколькими внешними у нас нет, но
            # на всякий случай — к первому.
            if polys:
                polys[0] += inner
        if not polys:
            print("нет контура у", corp["name"])
            continue
        geom = {"type": "MultiPolygon", "coordinates": [[list(map(list, r)) for r in p] for p in polys]}
        bid = "building-" + corp["id"]
        features.append({"type": "Feature", "id": bid, "feature_type": "building", "geometry": None,
                         "properties": {"category": "unspecified", "name": {"ru": corp["name"]},
                                        "osm": f"{e['type']}/{e['id']}"}})
        features.append({"type": "Feature", "id": "footprint-" + corp["id"], "feature_type": "footprint",
                         "geometry": geom, "properties": {"category": "ground", "building_ids": [bid]}})
        for lv in range(1, corp["levels"] + 1):
            features.append({"type": "Feature", "id": f"level-{corp['id']}-{lv}", "feature_type": "level",
                             "geometry": geom,
                             "properties": {"category": "unspecified", "ordinal": lv - 1, "short_name": {"ru": str(lv)},
                                            "name": {"ru": f"{lv} этаж"}, "building_ids": [bid],
                                            "outdoor": False}})
        print(f"{corp['name']}: колец {sum(len(p) for p in polys)}, этажей {corp['levels']}")
    with open(dst, "w", encoding="utf-8") as f:
        json.dump({"type": "FeatureCollection", "features": features}, f, ensure_ascii=False, separators=(",", ":"))


if __name__ == "__main__":
    main(*sys.argv[1:])
