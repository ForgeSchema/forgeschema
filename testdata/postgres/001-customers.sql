CREATE TABLE public.customers (
 id bigint NOT NULL,
 customer_name text NOT NULL,
 email text,
 CONSTRAINT customers_pkey PRIMARY KEY (id)
);
CREATE INDEX customers_email_idx ON public.customers (email);
INSERT INTO public.customers VALUES
 (1, 'Ada Example', 'ada@example.invalid'),
 (2, 'Dana Example', NULL);
