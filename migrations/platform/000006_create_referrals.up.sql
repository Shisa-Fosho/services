CREATE TABLE IF NOT EXISTS referrals (
    referrer_address TEXT        NOT NULL,
    referred_address TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT referrals_pk PRIMARY KEY (referrer_address, referred_address),
    CONSTRAINT referrals_no_self_referral CHECK (referrer_address != referred_address)
);

CREATE INDEX idx_referrals_referrer ON referrals (referrer_address);
CREATE INDEX idx_referrals_referred ON referrals (referred_address);
