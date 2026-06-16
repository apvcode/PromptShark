#include <iostream>
#include <string>

// AgentSupervisor C++ Core
// Reads JSON lines from stdin, processes them (e.g., checks for loops, dedupes),
// and writes JSON responses to stdout.
// This allows Go to communicate with C++ via simple standard I/O streams without CGO overhead.

#include "LoopDetector.hpp"

LoopDetector detector;

void process_message(const std::string& message) {
    if (message.rfind("CHECK_LOOP\t", 0) == 0) {
        size_t t1 = message.find('\t');
        size_t t2 = message.find('\t', t1 + 1);
        
        if (t1 != std::string::npos && t2 != std::string::npos) {
            std::string function_name = message.substr(t1 + 1, t2 - t1 - 1);
            std::string arguments = message.substr(t2 + 1);
            
            if (detector.checkLoop(function_name, arguments)) {
                std::cout << "{\"status\": \"loop_detected\", \"message\": \"Agent is stuck calling " << function_name << "\"}" << std::endl;
            } else {
                std::cout << "{\"status\": \"ok\"}" << std::endl;
            }
        }
    } else if (message == "RESET") {
        detector.reset();
        std::cout << "{\"status\": \"reset\"}" << std::endl;
    } else {
        std::cout << "{\"status\": \"ignored\"}" << std::endl;
    }
}

int main() {
    std::string line;
    
    // Optimize standard I/O operations for performance
    std::ios_base::sync_with_stdio(false);
    std::cin.tie(NULL);

    // Continuous read loop from stdin (acting as IPC with Go proxy)
    while (std::getline(std::cin, line)) {
        // Simple termination commands
        if (line == "exit" || line == "quit") {
            break;
        }
        
        // Ignore empty lines
        if (line.empty()) {
            continue;
        }
        
        process_message(line);
    }
    
    return 0;
}
