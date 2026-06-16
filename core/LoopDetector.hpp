#ifndef LOOP_DETECTOR_HPP
#define LOOP_DETECTOR_HPP

#include <string>
#include <vector>
#include <functional>
#include <fstream>
#include <sstream>
#include <iostream>

// LoopDetector analyzes LLM tool calls and detects if an agent is stuck in an infinite loop.
class LoopDetector {
private:
    std::vector<size_t> hash_history;
    size_t LOOP_THRESHOLD = 3;
    bool IGNORE_ARGS = false;

    void loadConfig(const std::string& path) {
        std::ifstream file(path);
        if (!file.is_open()) return; // use defaults if no config
        
        std::string line;
        while (std::getline(file, line)) {
            if (line.find("THRESHOLD=") == 0) {
                try {
                    LOOP_THRESHOLD = std::stoull(line.substr(10));
                } catch(...) {}
            } else if (line.find("IGNORE_ARGS=") == 0) {
                IGNORE_ARGS = (line.substr(12) == "true");
            }
        }
    }

    // Computes hash of the function name and arguments. 
    size_t computeHash(const std::string& function_name, const std::string& arguments) const {
        std::hash<std::string> hasher;
        if (IGNORE_ARGS) {
            return hasher(function_name);
        }
        return hasher(function_name + "|" + arguments);
    }

public:
    LoopDetector() {
        // Load config from the same directory as the executable (or relative)
        loadConfig("../loop_config.txt");
    }

    // Returns true if the tool_call condition is met consecutively based on config
    bool checkLoop(const std::string& function_name, const std::string& arguments) {
        size_t current_hash = computeHash(function_name, arguments);
        hash_history.push_back(current_hash);

        if (hash_history.size() >= LOOP_THRESHOLD) {
            size_t n = hash_history.size();
            bool is_loop = true;
            for (size_t i = 1; i < LOOP_THRESHOLD; ++i) {
                if (hash_history[n - 1] != hash_history[n - 1 - i]) {
                    is_loop = false;
                    break;
                }
            }
            return is_loop;
        }
        return false;
    }
    
    void reset() {
        hash_history.clear();
    }
};

#endif // LOOP_DETECTOR_HPP
