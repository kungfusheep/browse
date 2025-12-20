-- Links table cleanup script
-- Removes junk entries that shouldn't be crawled

-- First, let's see what we're dealing with
.headers on
.mode column

SELECT 'Before cleanup:' as status;
SELECT COUNT(*) as total_links FROM links;

-- Delete phone numbers and tel: links
DELETE FROM links WHERE to_domain LIKE '+%';
DELETE FROM links WHERE to_domain LIKE 'tel:%';
SELECT 'Deleted phone numbers' as status, changes() as deleted;

-- Delete obviously broken entries
DELETE FROM links WHERE to_domain LIKE '!%';
DELETE FROM links WHERE to_domain LIKE '$%';
DELETE FROM links WHERE to_domain LIKE '(%';
DELETE FROM links WHERE to_domain LIKE '"%';
DELETE FROM links WHERE to_domain LIKE '.%';
DELETE FROM links WHERE to_domain LIKE '-%';
DELETE FROM links WHERE to_domain LIKE ',%';
DELETE FROM links WHERE to_domain LIKE '*%';
DELETE FROM links WHERE to_domain LIKE '#%';
DELETE FROM links WHERE to_domain LIKE '@%';
DELETE FROM links WHERE to_domain LIKE '?%';
DELETE FROM links WHERE to_domain LIKE '/%';
DELETE FROM links WHERE to_domain LIKE '\%';
DELETE FROM links WHERE to_domain LIKE '{%';
DELETE FROM links WHERE to_domain LIKE '[%';
SELECT 'Deleted entries starting with special chars' as status;

-- Delete entries without a dot (not valid domains)
DELETE FROM links WHERE to_domain NOT LIKE '%.%';
SELECT 'Deleted entries without dots' as status, changes() as deleted;

-- Delete entries that are too short or too long
DELETE FROM links WHERE LENGTH(to_domain) < 4;
DELETE FROM links WHERE LENGTH(to_domain) > 80;
SELECT 'Deleted too short/long entries' as status, changes() as deleted;

-- Delete spam farm subdomains
DELETE FROM links WHERE to_domain LIKE '%.51dzw.com';
DELETE FROM links WHERE to_domain LIKE '%.sxwmcc.com';
DELETE FROM links WHERE to_domain LIKE '%.9856.cn';
DELETE FROM links WHERE to_domain LIKE '%.hotic.51dzw.com';
SELECT 'Deleted 51dzw/sxwmcc spam' as status, changes() as deleted;

-- Delete low-quality blog platforms (user-generated spam magnets)
DELETE FROM links WHERE to_domain LIKE '%.livejournal.com';
DELETE FROM links WHERE to_domain LIKE '%.dreamwidth.org';
DELETE FROM links WHERE to_domain LIKE '%.insanejournal.com';
DELETE FROM links WHERE to_domain LIKE '%.greatestjournal.com';
DELETE FROM links WHERE to_domain LIKE '%.tumblr.com';
DELETE FROM links WHERE to_domain LIKE '%.blogspot.com';
DELETE FROM links WHERE to_domain LIKE '%.blogspot.%';
DELETE FROM links WHERE to_domain LIKE '%.wordpress.com';
DELETE FROM links WHERE to_domain LIKE '%.createblog.com';
DELETE FROM links WHERE to_domain LIKE '%.beeplog.%';
SELECT 'Deleted blog platform subdomains' as status, changes() as deleted;

-- Delete software download spam
DELETE FROM links WHERE to_domain LIKE '%.softonic.%';
DELETE FROM links WHERE to_domain LIKE '%.uptodown.%';
DELETE FROM links WHERE to_domain LIKE '%.informer.com';
SELECT 'Deleted software download spam' as status, changes() as deleted;

-- Delete other known spam platforms
DELETE FROM links WHERE to_domain LIKE '%.sikatika.com';
DELETE FROM links WHERE to_domain LIKE '%.qowap.com';
DELETE FROM links WHERE to_domain LIKE '%.listal.com';
DELETE FROM links WHERE to_domain LIKE '%.mykajabi.com';
DELETE FROM links WHERE to_domain LIKE '%.stck.me';
DELETE FROM links WHERE to_domain LIKE '%.izrablog.com';
DELETE FROM links WHERE to_domain LIKE '%.blogsidea.com';
DELETE FROM links WHERE to_domain LIKE '%.blogmazing.com';
DELETE FROM links WHERE to_domain LIKE '%.jsyinshanfu.com';
DELETE FROM links WHERE to_domain LIKE '%.oh-hotel.%';
DELETE FROM links WHERE to_domain LIKE '%.life3dblog.com';
SELECT 'Deleted other spam platforms' as status, changes() as deleted;

-- Delete common social/big platforms (we don't want to crawl these)
DELETE FROM links WHERE to_domain IN (
    'facebook.com', 'www.facebook.com', 'web.facebook.com', 'm.facebook.com',
    'twitter.com', 'www.twitter.com', 'mobile.twitter.com',
    'x.com', 'www.x.com',
    'instagram.com', 'www.instagram.com',
    'linkedin.com', 'www.linkedin.com',
    'youtube.com', 'www.youtube.com', 'm.youtube.com',
    'youtu.be',
    'tiktok.com', 'www.tiktok.com',
    'pinterest.com', 'www.pinterest.com',
    'reddit.com', 'www.reddit.com', 'old.reddit.com',
    'discord.com', 'discord.gg',
    'telegram.org', 't.me',
    'whatsapp.com', 'api.whatsapp.com', 'wa.me',
    'snapchat.com',
    'twitch.tv', 'www.twitch.tv'
);
SELECT 'Deleted social media platforms' as status, changes() as deleted;

-- Delete Google/Apple/Microsoft/Amazon domains
DELETE FROM links WHERE to_domain LIKE '%.google.%';
DELETE FROM links WHERE to_domain LIKE '%.googleapis.com';
DELETE FROM links WHERE to_domain LIKE '%.gstatic.com';
DELETE FROM links WHERE to_domain LIKE '%.apple.com';
DELETE FROM links WHERE to_domain LIKE '%.microsoft.com';
DELETE FROM links WHERE to_domain LIKE '%.amazon.%';
DELETE FROM links WHERE to_domain LIKE '%.amazonaws.com';
DELETE FROM links WHERE to_domain LIKE '%.cloudfront.net';
SELECT 'Deleted big tech domains' as status, changes() as deleted;

-- Delete URL shorteners
DELETE FROM links WHERE to_domain IN (
    'bit.ly', 'tinyurl.com', 't.co', 'goo.gl', 'ow.ly', 'is.gd',
    'buff.ly', 'j.mp', 'lnkd.in', 'db.tt', 'qr.ae', 'adf.ly',
    'cur.lv', 'ity.im', 'q.gs', 'po.st', 'bc.vc', 'su.pr'
);
SELECT 'Deleted URL shorteners' as status, changes() as deleted;

-- Delete suspicious TLDs that are mostly spam
DELETE FROM links WHERE to_domain LIKE '%.click';
DELETE FROM links WHERE to_domain LIKE '%.top' AND to_domain NOT LIKE '%.desktop.%';
DELETE FROM links WHERE to_domain LIKE '%.loan';
DELETE FROM links WHERE to_domain LIKE '%.work';
DELETE FROM links WHERE to_domain LIKE '%.gq';
DELETE FROM links WHERE to_domain LIKE '%.ml';
DELETE FROM links WHERE to_domain LIKE '%.cf';
DELETE FROM links WHERE to_domain LIKE '%.tk';
DELETE FROM links WHERE to_domain LIKE '%.ga';
DELETE FROM links WHERE to_domain LIKE '%.pw';
DELETE FROM links WHERE to_domain LIKE '%.xyz' AND LENGTH(to_domain) > 20;
SELECT 'Deleted suspicious TLDs' as status, changes() as deleted;

-- Delete entries with non-ASCII characters (Thai gambling spam etc)
DELETE FROM links WHERE to_domain GLOB '*[^ -~]*';
SELECT 'Deleted non-ASCII domains' as status, changes() as deleted;

-- Final count
SELECT 'After cleanup:' as status;
SELECT COUNT(*) as total_links FROM links;

-- Vacuum to reclaim space
VACUUM;
SELECT 'Vacuumed database' as status;
