-- Площадка аудитории.
--
-- Поле campus отвечало только на вопрос «Ленинградский или нет», из-за чего
-- двенадцать московских адресов схлопывались в одно значение «other», и
-- фильтр по нему показывал их вперемешку. site — уникальный ключ площадки.
ALTER TABLE auditoriums ADD COLUMN site TEXT NOT NULL DEFAULT 'other';
ALTER TABLE auditoriums ADD COLUMN site_label TEXT NOT NULL DEFAULT '';
ALTER TABLE auditoriums ADD COLUMN site_order SMALLINT NOT NULL DEFAULT 99;

CREATE INDEX auditoriums_site_idx ON auditoriums (site, floor, room)
    WHERE is_study_space;

DROP INDEX IF EXISTS auditoriums_campus_idx;
