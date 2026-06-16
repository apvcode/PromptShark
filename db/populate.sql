-- Clean existing data first
DELETE FROM Steps;
DELETE FROM Sessions;

-- Session 1: Code Loop Detection
INSERT INTO Sessions (id, timestamp, replay_target) VALUES ('session_loop_demo_1', '2026-06-15 15:45:00', 0);

INSERT INTO Steps (id, session_id, step_index, request_payload, response_payload, tokens_used) VALUES 
('step_l1', 'session_loop_demo_1', 1, 
'{"messages":[{"role":"user","content":"Optimize this python code and look for recursive depth issue: \n\ndef fib(n):\n    return fib(n-1) + fib(n-2)"}]}',
'{"choices":[{"message":{"role":"assistant","content":"I need to analyze the file to inspect the exact structure. Let me call the read_file tool.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\": \"fib.py\"}"}}]}}]}',
150),
('step_l2', 'session_loop_demo_1', 2, 
'{"messages":[{"role":"tool","content":"def fib(n):\n    return fib(n-1) + fib(n-2)"},{"role":"assistant","content":"Analyzing... Let me read it again to check for other functions."}]}',
'{"choices":[{"message":{"role":"assistant","content":"I will check the same file again.","tool_calls":[{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{\"path\": \"fib.py\"}"}}]}}]}',
280),
('step_l3', 'session_loop_demo_1', 3, 
'{"messages":[{"role":"tool","content":"def fib(n):\n    return fib(n-1) + fib(n-2)"}]}',
'{"choices":[{"message":{"role":"assistant","content":"🚨 [SYSTEM ALERT: INFINITE LOOP DETECTED BY C++ CORE] 🚨\n\nAgent is stuck calling tool \"read_file\" repeatedly with identical arguments: {\"path\": \"fib.py\"}."}}]}',
0);

-- Session 2: Web Scraping Agent
INSERT INTO Sessions (id, timestamp, replay_target) VALUES ('session_scrape_2', '2026-06-15 15:50:00', 0);

INSERT INTO Steps (id, session_id, step_index, request_payload, response_payload, tokens_used) VALUES 
('step_s1', 'session_scrape_2', 1, 
'{"messages":[{"role":"user","content":"Find the price of iPad Pro 2026 on Amazon"}]}',
'{"choices":[{"message":{"role":"assistant","content":"Let me search the web for the latest Amazon price listings.","tool_calls":[{"id":"call_s1","type":"function","function":{"name":"web_search","arguments":"{\"query\": \"site:amazon.com iPad Pro 2026 price\"}"}}]}}]}',
120),
('step_s2', 'session_scrape_2', 2, 
'{"messages":[{"role":"tool","content":"Search Results:\n1. Apple iPad Pro 11-Inch (M4): $999 on Amazon.com\n2. Apple iPad Pro 13-Inch (M4): $1299 on Amazon.com"}]}',
'{"choices":[{"message":{"role":"assistant","content":"I found the iPad Pro listings on Amazon. The 11-inch model with M4 chip is priced at $999, and the 13-inch model is priced at $1,299."}}]}',
310);

-- Session 3: Time-Travel Email Draft
INSERT INTO Sessions (id, timestamp, replay_target) VALUES ('session_time_travel_3', '2026-06-15 15:58:00', 0);

INSERT INTO Steps (id, session_id, step_index, request_payload, response_payload, tokens_used) VALUES 
('step_t1', 'session_time_travel_3', 1, 
'{"messages":[{"role":"user","content":"Draft an email to Bob about the upcoming Q3 release delay"}]}',
'{"choices":[{"message":{"role":"assistant","content":"Dear Bob,\n\nI am writing to formally inform you that the Q3 product release has been rescheduled due to unexpected QA cycles...\n\nBest regards,\nAgent."}}]}',
180),
('step_t2', 'session_time_travel_3', 2, 
'{"messages":[{"role":"user","content":"Draft an email to Bob about the upcoming Q3 release delay. MAKE IT EXTRA CASUAL."}]}',
'{"choices":[{"message":{"role":"assistant","content":"Hey Bob,\n\nJust wanted to head-up you that we are pushing the Q3 release a bit to polish some bugs. Talk soon!\n\nCheers."}}]}',
210);
