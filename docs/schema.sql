--
-- PostgreSQL database dump
--


-- Dumped from database version 16.13
-- Dumped by pg_dump version 16.13

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
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: uuid-ossp; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public;


--
-- Name: EXTENSION "uuid-ossp"; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION "uuid-ossp" IS 'generate universally unique identifiers (UUIDs)';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: affiliate_earnings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.affiliate_earnings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    referrer_address text NOT NULL,
    trade_id uuid NOT NULL,
    fee_amount bigint NOT NULL,
    referrer_cut bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT affiliate_earnings_cut_positive CHECK ((referrer_cut > 0)),
    CONSTRAINT affiliate_earnings_fee_positive CHECK ((fee_amount > 0))
);


--
-- Name: balances; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.balances (
    user_address text NOT NULL,
    available bigint DEFAULT 0 NOT NULL,
    reserved bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT balances_available_non_negative CHECK ((available >= 0)),
    CONSTRAINT balances_reserved_non_negative CHECK ((reserved >= 0))
);


--
-- Name: categories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.categories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL
);


--
-- Name: events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    category_id uuid,
    event_type smallint NOT NULL,
    resolution_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    status smallint DEFAULT 0 NOT NULL,
    end_date timestamp with time zone NOT NULL,
    featured boolean DEFAULT false NOT NULL,
    featured_sort_order smallint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: markets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.markets (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    event_id uuid,
    question text NOT NULL,
    outcome_yes_label text DEFAULT 'Yes'::text NOT NULL,
    outcome_no_label text DEFAULT 'No'::text NOT NULL,
    token_id_yes text NOT NULL,
    token_id_no text NOT NULL,
    condition_id text NOT NULL,
    status smallint DEFAULT 0 NOT NULL,
    outcome smallint,
    price_yes bigint DEFAULT 50 NOT NULL,
    price_no bigint DEFAULT 50 NOT NULL,
    volume bigint DEFAULT 0 NOT NULL,
    open_interest bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT markets_open_interest_non_negative CHECK ((open_interest >= 0)),
    CONSTRAINT markets_volume_non_negative CHECK ((volume >= 0))
);


--
-- Name: orders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.orders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    maker text NOT NULL,
    token_id text NOT NULL,
    maker_amount bigint NOT NULL,
    taker_amount bigint NOT NULL,
    salt text NOT NULL,
    expiration bigint DEFAULT 0 NOT NULL,
    nonce bigint DEFAULT 0 NOT NULL,
    fee_rate_bps bigint DEFAULT 0 NOT NULL,
    side smallint NOT NULL,
    signature_type smallint DEFAULT 0 NOT NULL,
    signature text NOT NULL,
    status smallint DEFAULT 0 NOT NULL,
    order_type smallint DEFAULT 0 NOT NULL,
    market_id uuid NOT NULL,
    signature_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: positions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.positions (
    user_address text NOT NULL,
    market_id uuid NOT NULL,
    side smallint NOT NULL,
    size bigint DEFAULT 0 NOT NULL,
    average_entry_price bigint DEFAULT 0 NOT NULL,
    realised_pnl bigint DEFAULT 0 NOT NULL,
    CONSTRAINT positions_size_non_negative CHECK ((size >= 0))
);


--
-- Name: referrals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.referrals (
    referrer_address text NOT NULL,
    referred_address text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT referrals_no_self_referral CHECK ((referrer_address <> referred_address))
);


--
-- Name: schema_migrations_platform; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations_platform (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: schema_migrations_shared; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations_shared (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: schema_migrations_trading; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations_trading (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: trades; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trades (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    match_id text NOT NULL,
    maker_order_id uuid NOT NULL,
    taker_order_id uuid NOT NULL,
    maker_address text NOT NULL,
    taker_address text NOT NULL,
    market_id uuid NOT NULL,
    price bigint NOT NULL,
    size bigint NOT NULL,
    maker_fee bigint DEFAULT 0 NOT NULL,
    taker_fee bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    address text NOT NULL,
    username text NOT NULL,
    email text,
    signup_method smallint NOT NULL,
    safe_address text DEFAULT ''::text NOT NULL,
    proxy_address text DEFAULT ''::text NOT NULL,
    twofa_secret_encrypted text DEFAULT ''::text NOT NULL,
    twofa_enabled boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: affiliate_earnings affiliate_earnings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.affiliate_earnings
    ADD CONSTRAINT affiliate_earnings_pkey PRIMARY KEY (id);


--
-- Name: affiliate_earnings affiliate_earnings_trade_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.affiliate_earnings
    ADD CONSTRAINT affiliate_earnings_trade_unique UNIQUE (trade_id);


--
-- Name: balances balances_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.balances
    ADD CONSTRAINT balances_pkey PRIMARY KEY (user_address);


--
-- Name: categories categories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.categories
    ADD CONSTRAINT categories_pkey PRIMARY KEY (id);


--
-- Name: categories categories_slug_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.categories
    ADD CONSTRAINT categories_slug_unique UNIQUE (slug);


--
-- Name: events events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.events
    ADD CONSTRAINT events_pkey PRIMARY KEY (id);


--
-- Name: events events_slug_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.events
    ADD CONSTRAINT events_slug_unique UNIQUE (slug);


--
-- Name: markets markets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.markets
    ADD CONSTRAINT markets_pkey PRIMARY KEY (id);


--
-- Name: markets markets_slug_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.markets
    ADD CONSTRAINT markets_slug_unique UNIQUE (slug);


--
-- Name: orders orders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);


--
-- Name: orders orders_signature_hash_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_signature_hash_unique UNIQUE (signature_hash);


--
-- Name: positions positions_pk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_pk PRIMARY KEY (user_address, market_id, side);


--
-- Name: referrals referrals_pk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.referrals
    ADD CONSTRAINT referrals_pk PRIMARY KEY (referrer_address, referred_address);


--
-- Name: schema_migrations_platform schema_migrations_platform_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations_platform
    ADD CONSTRAINT schema_migrations_platform_pkey PRIMARY KEY (version);


--
-- Name: schema_migrations_shared schema_migrations_shared_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations_shared
    ADD CONSTRAINT schema_migrations_shared_pkey PRIMARY KEY (version);


--
-- Name: schema_migrations_trading schema_migrations_trading_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations_trading
    ADD CONSTRAINT schema_migrations_trading_pkey PRIMARY KEY (version);


--
-- Name: trades trades_match_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trades
    ADD CONSTRAINT trades_match_id_unique UNIQUE (match_id);


--
-- Name: trades trades_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trades
    ADD CONSTRAINT trades_pkey PRIMARY KEY (id);


--
-- Name: users users_email_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_unique UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (address);


--
-- Name: users users_username_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_username_unique UNIQUE (username);


--
-- Name: idx_affiliate_earnings_referrer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_affiliate_earnings_referrer ON public.affiliate_earnings USING btree (referrer_address);


--
-- Name: idx_events_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_events_category ON public.events USING btree (category_id);


--
-- Name: idx_events_featured; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_events_featured ON public.events USING btree (featured, featured_sort_order) WHERE (featured = true);


--
-- Name: idx_events_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_events_status ON public.events USING btree (status);


--
-- Name: idx_markets_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_markets_event ON public.markets USING btree (event_id);


--
-- Name: idx_markets_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_markets_status ON public.markets USING btree (status);


--
-- Name: idx_orders_market_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_market_status ON public.orders USING btree (market_id, status);


--
-- Name: idx_orders_user_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_user_status ON public.orders USING btree (maker, status);


--
-- Name: idx_positions_market; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_positions_market ON public.positions USING btree (market_id);


--
-- Name: idx_positions_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_positions_user ON public.positions USING btree (user_address);


--
-- Name: idx_referrals_referred; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_referrals_referred ON public.referrals USING btree (referred_address);


--
-- Name: idx_referrals_referrer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_referrals_referrer ON public.referrals USING btree (referrer_address);


--
-- Name: idx_trades_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trades_created_at ON public.trades USING btree (created_at);


--
-- Name: idx_trades_maker_address; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trades_maker_address ON public.trades USING btree (maker_address);


--
-- Name: idx_trades_market_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trades_market_id ON public.trades USING btree (market_id);


--
-- Name: idx_trades_market_maker; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trades_market_maker ON public.trades USING btree (market_id, maker_address);


--
-- Name: idx_trades_market_taker; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trades_market_taker ON public.trades USING btree (market_id, taker_address);


--
-- Name: idx_trades_taker_address; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trades_taker_address ON public.trades USING btree (taker_address);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_email ON public.users USING btree (email) WHERE (email IS NOT NULL);


--
-- Name: events events_category_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.events
    ADD CONSTRAINT events_category_id_fkey FOREIGN KEY (category_id) REFERENCES public.categories(id);


--
-- Name: markets markets_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.markets
    ADD CONSTRAINT markets_event_id_fkey FOREIGN KEY (event_id) REFERENCES public.events(id);


--
-- Name: positions positions_market_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_market_id_fkey FOREIGN KEY (market_id) REFERENCES public.markets(id);


--
-- Name: positions positions_user_address_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_user_address_fkey FOREIGN KEY (user_address) REFERENCES public.users(address);


--
-- Name: trades trades_maker_order_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trades
    ADD CONSTRAINT trades_maker_order_id_fkey FOREIGN KEY (maker_order_id) REFERENCES public.orders(id);


--
-- Name: trades trades_taker_order_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trades
    ADD CONSTRAINT trades_taker_order_id_fkey FOREIGN KEY (taker_order_id) REFERENCES public.orders(id);


--
-- PostgreSQL database dump complete
--


