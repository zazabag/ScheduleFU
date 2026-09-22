-- Привязка пар к группам, дотянутая из расписания самой группы у вуза.
-- Слепок по аудиториям не знает, чья это языковая подгруппа: имя потока
-- «006126_2 Иностранный язык (КАЯиПК)-3» базовой группы не содержит. Раз в
-- день для группы, которую кто-то открыл, спрашиваем вуз и запоминаем
-- связь. Пары не копируем: те же lesson_oid, только принадлежность.
CREATE TABLE group_links (
    group_name TEXT   NOT NULL,
    lesson_oid BIGINT NOT NULL,
    PRIMARY KEY (group_name, lesson_oid)
);
CREATE INDEX group_links_lesson_idx ON group_links (lesson_oid);

CREATE TABLE group_fetches (
    group_name TEXT PRIMARY KEY,
    fetched_on DATE NOT NULL
);
