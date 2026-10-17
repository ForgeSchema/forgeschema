-- Hand-written DDL matching examples/shop.yaml, used to check that inspection
-- maps ordinary PostgreSQL DDL onto the documented schema contract.
CREATE TABLE public.customers (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email varchar(320) NOT NULL CONSTRAINT customers_email_key UNIQUE,
  display_name text,
  status varchar(20) NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE public.orders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id bigint NOT NULL CONSTRAINT orders_customer_fkey REFERENCES public.customers(id) ON DELETE RESTRICT,
  total numeric(12,2) NOT NULL DEFAULT 0,
  paid boolean NOT NULL DEFAULT false,
  placed_on date NOT NULL DEFAULT CURRENT_DATE,
  metadata jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX orders_customer_idx ON public.orders(customer_id, placed_on);
CREATE TABLE public.order_lines (
  order_id uuid NOT NULL CONSTRAINT order_lines_order_fkey REFERENCES public.orders(id) ON DELETE CASCADE,
  line_no smallint NOT NULL,
  sku char(12) NOT NULL,
  quantity integer NOT NULL DEFAULT 1,
  PRIMARY KEY (order_id, line_no)
);
CREATE UNIQUE INDEX order_lines_sku_idx ON public.order_lines(sku, order_id);
INSERT INTO public.customers(email, display_name) VALUES ('ada@example.com', 'Ada'), ('dana@example.com', NULL);
INSERT INTO public.orders(customer_id, total) VALUES (1, 19.90);
INSERT INTO public.order_lines(order_id, line_no, sku) SELECT id, 1, 'SKU-0000001' FROM public.orders;
