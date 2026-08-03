--
-- PostgreSQL database dump
--

\restrict vA4owehlMbEgs3ae4zMBcCxkyibDRS0FVZfIvIDbrmlwdjMJvRJXtXvmwJJgwbQ

-- Dumped from database version 16.11
-- Dumped by pg_dump version 16.11

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: fixturedb; Type: DATABASE; Schema: -; Owner: postgres
--

CREATE DATABASE fixturedb WITH TEMPLATE = template0 ENCODING = 'UTF8' LOCALE_PROVIDER = libc LOCALE = 'en_US.utf8';


ALTER DATABASE fixturedb OWNER TO postgres;

\unrestrict vA4owehlMbEgs3ae4zMBcCxkyibDRS0FVZfIvIDbrmlwdjMJvRJXtXvmwJJgwbQ
\connect fixturedb
\restrict vA4owehlMbEgs3ae4zMBcCxkyibDRS0FVZfIvIDb